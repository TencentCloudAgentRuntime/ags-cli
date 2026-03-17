// Package pty provides native PTY terminal session support for sandbox instances.
// It uses the envd component's PTY RPC capability directly, without requiring ttyd or a browser.
// The design mirrors E2B's terminal.ts approach: create PTY, batch-flush stdin, handle SIGWINCH.
package pty

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"connectrpc.com/connect"
	"github.com/TencentCloudAgentRuntime/ags-go-sdk/connection"
	"github.com/TencentCloudAgentRuntime/ags-go-sdk/constant"
	"github.com/TencentCloudAgentRuntime/ags-go-sdk/pb/process"
	"github.com/TencentCloudAgentRuntime/ags-go-sdk/pb/process/processconnect"
	"github.com/TencentCloudAgentRuntime/ags-go-sdk/sandbox/core"
	"golang.org/x/term"
)

const (
	// flushInputInterval is the interval for batching and flushing PTY input,
	// mirroring the approach used by E2B's terminal.ts (FLUSH_INPUT_INTERVAL_MS = 10ms).
	flushInputInterval = 10 * time.Millisecond

	// defaultShell is the shell to spawn in the PTY session
	defaultShell = "/bin/bash"

	// keepalivePingIntervalSeconds is set on the Start RPC to keep the stream alive
	keepalivePingIntervalSeconds = "30"
)

// Session manages a PTY terminal session against an AGS sandbox instance.
type Session struct {
	accessToken string
	domain      string
}

// NewSession creates a new PTY session manager with the given data-plane credentials.
func NewSession(accessToken, domain string) *Session {
	return &Session{
		accessToken: accessToken,
		domain:      domain,
	}
}

// Connect opens an interactive PTY session in the given sandbox instance.
// It puts the local terminal into raw mode, forwards all stdin to the remote PTY,
// streams remote output to stdout, and propagates terminal resize events (SIGWINCH).
// The function blocks until the remote session ends or ctx is cancelled.
func (s *Session) Connect(ctx context.Context, instanceID, user string) error {
	envdHost := s.envdHost(instanceID)

	// Build the process RPC client that speaks to envd
	rpcClient := newProcessClient(envdHost)

	// Determine initial terminal size
	cols, rows := termSize()

	// Put local terminal into raw mode before starting the remote PTY so that
	// all keystrokes (including Ctrl-C, arrows, etc.) are forwarded verbatim.
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("failed to set terminal to raw mode: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState) //nolint:errcheck

	// Start a PTY-enabled bash process inside the sandbox.
	startReq := connect.NewRequest(&process.StartRequest{
		Process: &process.ProcessConfig{
			Cmd: defaultShell,
		},
		Pty: &process.PTY{
			Size: &process.PTY_Size{
				Cols: uint32(cols),
				Rows: uint32(rows),
			},
		},
	})
	startReq.Header().Set("X-Access-Token", s.accessToken)
	// Basic auth header selects the user inside the sandbox (same convention as the SDK)
	startReq.Header().Set(
		"Authorization",
		fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString([]byte(user+":"))),
	)
	startReq.Header().Set("Keepalive-Ping-Interval", keepalivePingIntervalSeconds)

	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, err := rpcClient.Start(cancelCtx, startReq)
	if err != nil {
		return fmt.Errorf("failed to start PTY process: %w", err)
	}

	// First event must be a StartEvent that gives us the PID
	if !stream.Receive() {
		if err := stream.Err(); err != nil {
			return fmt.Errorf("PTY stream error on start: %w", err)
		}
		return fmt.Errorf("PTY stream closed before start event")
	}
	startMsg := stream.Msg()
	if startMsg == nil || startMsg.Event == nil {
		return fmt.Errorf("unexpected nil start message from PTY stream")
	}
	startEv := startMsg.Event.GetStart()
	if startEv == nil {
		return fmt.Errorf("first PTY event is not a start event")
	}
	pid := startEv.GetPid()

	// --- Goroutine: stream remote PTY output → local stdout ---
	sessionDone := make(chan error, 1)
	go func() {
		for stream.Receive() {
			msg := stream.Msg()
			if msg == nil || msg.Event == nil {
				continue
			}
			switch ev := msg.Event.Event.(type) {
			case *process.ProcessEvent_Data:
				if d := ev.Data; d != nil {
					// When PTY is enabled, terminal output arrives on the Pty field
					// (not Stdout/Stderr which are used for non-PTY processes).
					if out := d.GetPty(); len(out) > 0 {
						_, _ = os.Stdout.Write(out)
					}
				}
			case *process.ProcessEvent_End:
				exitCode := int32(0)
				if ev.End != nil {
					exitCode = ev.End.GetExitCode()
				}
				if exitCode != 0 {
					sessionDone <- fmt.Errorf("PTY session exited with code %d", exitCode)
				} else {
					sessionDone <- nil
				}
				return
			}
		}
		if err := stream.Err(); err != nil {
			sessionDone <- err
		} else {
			sessionDone <- nil
		}
	}()

	// --- Goroutine: batch-flush local stdin → remote PTY (mirrors BatchedQueue in E2B) ---
	inputCh := make(chan []byte, 128)
	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		ticker := time.NewTicker(flushInputInterval)
		defer ticker.Stop()
		var pending [][]byte
		flush := func() {
			if len(pending) == 0 {
				return
			}
			data := concat(pending)
			pending = pending[:0]
			sendPTYInput(cancelCtx, rpcClient, pid, data, s.accessToken)
		}
		for {
			select {
			case data, ok := <-inputCh:
				if !ok {
					flush()
					return
				}
				pending = append(pending, data)
			case <-ticker.C:
				flush()
			case <-cancelCtx.Done():
				flush()
				return
			}
		}
	}()

	// --- Goroutine: read local stdin → inputCh ---
	stdinDone := make(chan struct{})
	go func() {
		defer close(stdinDone)
		buf := make([]byte, 1024)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				data := make([]byte, n)
				copy(data, buf[:n])
				select {
				case inputCh <- data:
				case <-cancelCtx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// --- Goroutine: propagate SIGWINCH → remote PTY resize ---
	sigwinchCh := make(chan os.Signal, 1)
	signal.Notify(sigwinchCh, syscall.SIGWINCH)
	resizeDone := make(chan struct{})
	go func() {
		defer func() {
			signal.Stop(sigwinchCh)
			close(resizeDone)
		}()
		for {
			select {
			case <-sigwinchCh:
				newCols, newRows := termSize()
				resizePTY(cancelCtx, rpcClient, pid, uint32(newCols), uint32(newRows), s.accessToken)
			case <-cancelCtx.Done():
				return
			}
		}
	}()

	// Wait for the PTY session to finish
	var sessionErr error
	select {
	case sessionErr = <-sessionDone:
	case <-ctx.Done():
		sessionErr = ctx.Err()
	}

	// Signal all goroutines to stop
	cancel()
	close(inputCh)
	<-inputDone
	<-resizeDone
	// stdinDone will unblock once the Stdin.Read returns (next keypress or EOF)

	// Print a newline so the shell prompt appears on a fresh line after restore
	fmt.Println()

	return sessionErr
}

// envdHost returns the envd hostname for the given instance.
func (s *Session) envdHost(instanceID string) string {
	c := core.NewCore(nil, instanceID, &connection.Config{
		Domain:      s.domain,
		AccessToken: s.accessToken,
	})
	return c.GetHost(constant.EnvdPort)
}

// newProcessClient builds a processconnect.ProcessClient that connects to envdHost.
func newProcessClient(envdHost string) processconnect.ProcessClient {
	httpClient := &http.Client{}
	baseURL := fmt.Sprintf("https://%s", envdHost)
	return processconnect.NewProcessClient(httpClient, baseURL, connect.WithProtoJSON())
}

// sendPTYInput sends PTY keyboard data to a running process via the SendInput RPC.
func sendPTYInput(ctx context.Context, cli processconnect.ProcessClient, pid uint32, data []byte, accessToken string) {
	req := connect.NewRequest(&process.SendInputRequest{
		Process: &process.ProcessSelector{
			Selector: &process.ProcessSelector_Pid{Pid: pid},
		},
		Input: &process.ProcessInput{
			Input: &process.ProcessInput_Pty{Pty: data},
		},
	})
	req.Header().Set("X-Access-Token", accessToken)
	_, _ = cli.SendInput(ctx, req)
}

// resizePTY sends a PTY resize request for the given process.
func resizePTY(ctx context.Context, cli processconnect.ProcessClient, pid uint32, cols, rows uint32, accessToken string) {
	req := connect.NewRequest(&process.UpdateRequest{
		Process: &process.ProcessSelector{
			Selector: &process.ProcessSelector_Pid{Pid: pid},
		},
		Pty: &process.PTY{
			Size: &process.PTY_Size{
				Cols: cols,
				Rows: rows,
			},
		},
	})
	req.Header().Set("X-Access-Token", accessToken)
	_, _ = cli.Update(ctx, req)
}

// termSize returns the current terminal width and height, defaulting to 220×50 on error.
func termSize() (int, int) {
	cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || cols <= 0 || rows <= 0 {
		return 220, 50
	}
	return cols, rows
}

// concat joins multiple byte slices into one.
func concat(bufs [][]byte) []byte {
	total := 0
	for _, b := range bufs {
		total += len(b)
	}
	out := make([]byte, 0, total)
	for _, b := range bufs {
		out = append(out, b...)
	}
	return out
}

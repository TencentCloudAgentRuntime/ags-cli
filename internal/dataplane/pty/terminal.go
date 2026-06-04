// Package pty provides native PTY terminal session support for sandbox instances.
// It uses the envd component's PTY RPC capability directly, without requiring ttyd or a browser.
// The design mirrors legacy's terminal.ts approach: create PTY, batch-flush stdin, handle SIGWINCH.
package pty

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
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
	// mirroring the approach used by legacy's terminal.ts (FLUSH_INPUT_INTERVAL_MS = 10ms).
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

// sessionOutcome carries the remote PTY end state to the foreground goroutine.
// exitCode is the remote shell's final exit status (0 for a clean `exit`).
// err is non-nil only for transport-level failures (envd RPC, broken stream).
// The two are independent: a non-zero exitCode with err == nil is a routine
// remote exit (e.g. user pressed Ctrl-C then typed `exit`, leaving bash with
// status 130) and must be propagated without being rendered as a CLI error.
type sessionOutcome struct {
	exitCode int
	err      error
}

// Connect opens an interactive PTY session in the given sandbox instance.
// It puts the local terminal into raw mode, forwards all stdin to the remote PTY,
// streams remote output to stdout, and propagates terminal resize events (SIGWINCH).
// The function blocks until the remote session ends or ctx is cancelled.
//
// The first return value is the remote shell's exit code (0 on a clean exit).
// The second return value is non-nil only for transport-level failures, never
// for a non-zero remote exit. Callers should propagate the exit code as the
// CLI process exit code without rendering an error envelope.
func (s *Session) Connect(ctx context.Context, instanceID, user string) (int, error) {
	envdHost := s.envdHost(instanceID)

	// Build the process RPC client that speaks to envd
	rpcClient := newProcessClient(envdHost)

	// Determine initial terminal size
	cols, rows := termSize()

	// Put local terminal into raw mode before starting the remote PTY so that
	// all keystrokes (including Ctrl-C, arrows, etc.) are forwarded verbatim.
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return 0, fmt.Errorf("failed to set terminal to raw mode: %w", err)
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
	setAccessToken(startReq.Header(), s.accessToken)
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
		return 0, fmt.Errorf("failed to start PTY process: %w", err)
	}

	// First event must be a StartEvent that gives us the PID
	if !stream.Receive() {
		if err := stream.Err(); err != nil {
			return 0, fmt.Errorf("PTY stream error on start: %w", err)
		}
		return 0, fmt.Errorf("PTY stream closed before start event")
	}
	startMsg := stream.Msg()
	if startMsg == nil || startMsg.Event == nil {
		return 0, fmt.Errorf("unexpected nil start message from PTY stream")
	}
	startEv := startMsg.Event.GetStart()
	if startEv == nil {
		return 0, fmt.Errorf("first PTY event is not a start event")
	}
	pid := startEv.GetPid()

	// --- Goroutine: stream remote PTY output → local stdout ---
	sessionDone := make(chan sessionOutcome, 1)
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
				var exitCode int32
				if ev.End != nil {
					exitCode = ev.End.GetExitCode()
				}
				// A remote shell exit is a clean session end regardless of
				// the exit code: the user typed `exit` (possibly inheriting
				// 130 from a Ctrl-C'd command). Propagate the code without
				// flagging this as a CLI error.
				sessionDone <- sessionOutcome{exitCode: int(exitCode)}
				return
			}
		}
		if err := stream.Err(); err != nil {
			sessionDone <- sessionOutcome{err: err}
		} else {
			sessionDone <- sessionOutcome{}
		}
	}()

	// --- Goroutine: batch-flush local stdin → remote PTY (mirrors BatchedQueue in legacy) ---
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

	resizeDone := startResizeWatcher(cancelCtx, rpcClient, pid, s.accessToken)

	// Wait for the PTY session to finish
	var outcome sessionOutcome
	select {
	case outcome = <-sessionDone:
	case <-ctx.Done():
		outcome = sessionOutcome{err: ctx.Err()}
	}

	// Signal all goroutines to stop
	cancel()
	close(inputCh)
	<-inputDone
	<-resizeDone
	// stdinDone will unblock once the Stdin.Read returns (next keypress or EOF)

	// Print a newline so the shell prompt appears on a fresh line after restore
	fmt.Println()

	return outcome.exitCode, outcome.err
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
	setAccessToken(req.Header(), accessToken)
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
	setAccessToken(req.Header(), accessToken)
	_, _ = cli.Update(ctx, req)
}

// setAccessToken sets the X-Access-Token header when accessToken is non-empty.
// A blank token means the target sandbox does not require authentication
// (e.g. Cloud AuthMode=NONE or legacy instances without an envdAccessToken),
// in which case the data plane does not require (and may reject) the header.
func setAccessToken(h http.Header, accessToken string) {
	if accessToken == "" {
		return
	}
	h.Set("X-Access-Token", accessToken)
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

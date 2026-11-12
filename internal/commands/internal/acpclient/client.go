//go:build preview

package acpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// AgentInfo is the part of the initialize result useful for diagnostics.
type AgentInfo struct {
	ProtocolVersion int    `json:"ProtocolVersion"`
	Name            string `json:"Name,omitempty"`
	Version         string `json:"Version,omitempty"`
	LoadSession     bool   `json:"LoadSession"`
}

// Session is a live ACP connection driven through the bridge.
type Session struct {
	Conn   *acp.ClientSideConnection
	Bridge *Bridge
	Info   AgentInfo

	cancel  context.CancelFunc
	done    chan error
	closers []io.Closer
}

// Handler receives session updates and permission requests.
type Handler interface {
	SessionUpdate(context.Context, acp.SessionNotification) error
	// RequestPermission answers a tool permission request.
	RequestPermission(context.Context, acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error)
}

// Connect starts a bridge over in-memory pipes and initializes ACP through the
// SDK, so the SDK owns JSON-RPC framing, IDs and typed messages.
func Connect(ctx context.Context, endpoint Endpoint, handler Handler) (*Session, error) {
	clientToBridgeR, clientToBridgeW := io.Pipe()
	bridgeToClientR, bridgeToClientW := io.Pipe()
	bridge := NewBridge(endpoint, bridgeToClientW)
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	session := &Session{Bridge: bridge, cancel: cancel, done: make(chan error, 1), closers: []io.Closer{clientToBridgeW, bridgeToClientW}}
	go func() {
		err := bridge.Run(runCtx, clientToBridgeR)
		_ = bridgeToClientW.CloseWithError(errOrEOF(err))
		session.done <- err
	}()
	session.Conn = acp.NewClientSideConnection(sdkClient{handler}, clientToBridgeW, bridgeToClientR)
	session.Conn.SetLogger(discardLogger)
	init, err := session.Conn.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion:    acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{},
		ClientInfo:         &acp.Implementation{Name: "agr", Version: "preview"},
	})
	if err != nil {
		err = session.wrap(err)
		session.Close()
		return nil, err
	}
	session.Info = AgentInfo{ProtocolVersion: int(init.ProtocolVersion), LoadSession: init.AgentCapabilities.LoadSession}
	if init.AgentInfo != nil {
		session.Info.Name, session.Info.Version = init.AgentInfo.Name, init.AgentInfo.Version
	}
	return session, nil
}

// wrap prefers the bridge's transport error, which explains why the SDK
// connection dropped. The SDK can observe the closed pipe slightly before the
// bridge goroutine reports, so wait briefly for that cause.
func (s *Session) wrap(err error) error {
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	select {
	case bridgeErr := <-s.done:
		s.done <- bridgeErr
		if bridgeErr != nil && !errors.Is(bridgeErr, context.Canceled) {
			return bridgeErr
		}
	case <-timer.C:
	}
	return err
}

// Err wraps an SDK error with the underlying transport cause when present.
func (s *Session) Err(err error) error { return s.wrap(err) }

// Classify turns a transport failure into a structured CLI error, keeping
// platform HTTP details; other errors become a retryable network failure.
func Classify(err error, hint string) error {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.CLIError(hint)
	}
	var cliErr *output.CLIError
	if errors.As(err, &cliErr) {
		return err
	}
	return output.NewCLIError(&output.Failure{Code: "ACP_UNREACHABLE", Kind: output.KindNetwork, Message: err.Error(), Hint: hint, Retryable: true}).WithCause(err)
}

// Close stops the bridge and releases the remote connection.
func (s *Session) Close() {
	for _, closer := range s.closers {
		_ = closer.Close()
	}
	s.cancel()
	err := <-s.done
	s.done <- err
}

func errOrEOF(err error) error {
	if err == nil {
		return io.EOF
	}
	return err
}

// Probe performs one initialize handshake and closes the connection.
func Probe(ctx context.Context, endpoint Endpoint) (AgentInfo, error) {
	session, err := Connect(ctx, endpoint, denyAll{})
	if err != nil {
		return AgentInfo{}, err
	}
	defer session.Close()
	return session.Info, nil
}

// ServeStdio bridges a local stdio ACP client to the endpoint.
func ServeStdio(ctx context.Context, endpoint Endpoint, in io.Reader, out io.Writer) (*Bridge, error) {
	bridge := NewBridge(endpoint, out)
	return bridge, bridge.Run(ctx, bufio.NewReader(in))
}

// sdkClient adapts Handler to acp.Client. Capabilities the CLI never
// advertises (files, terminals) reject with method-not-found semantics.
type sdkClient struct{ handler Handler }

func (c sdkClient) SessionUpdate(ctx context.Context, params acp.SessionNotification) error {
	return c.handler.SessionUpdate(ctx, params)
}

func (c sdkClient) RequestPermission(ctx context.Context, params acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	return c.handler.RequestPermission(ctx, params)
}

var errUnsupported = errors.New("not supported by agr")

func (sdkClient) ReadTextFile(context.Context, acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	return acp.ReadTextFileResponse{}, errUnsupported
}

func (sdkClient) WriteTextFile(context.Context, acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	return acp.WriteTextFileResponse{}, errUnsupported
}

func (sdkClient) CreateTerminal(context.Context, acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	return acp.CreateTerminalResponse{}, errUnsupported
}

func (sdkClient) KillTerminal(context.Context, acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	return acp.KillTerminalResponse{}, errUnsupported
}

func (sdkClient) TerminalOutput(context.Context, acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	return acp.TerminalOutputResponse{}, errUnsupported
}

func (sdkClient) ReleaseTerminal(context.Context, acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	return acp.ReleaseTerminalResponse{}, errUnsupported
}

func (sdkClient) WaitForTerminalExit(context.Context, acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	return acp.WaitForTerminalExitResponse{}, errUnsupported
}

// denyAll answers every permission request with cancelled and drops updates.
type denyAll struct{}

func (denyAll) SessionUpdate(context.Context, acp.SessionNotification) error { return nil }

func (denyAll) RequestPermission(context.Context, acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	return Cancelled(), nil
}

// Cancelled is the permission outcome used when no human can approve.
func Cancelled() acp.RequestPermissionResponse {
	return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeCancelled()}
}

// TextOf returns the text of a content block, or a short placeholder.
func TextOf(block acp.ContentBlock) string {
	switch {
	case block.Text != nil:
		return block.Text.Text
	case block.ResourceLink != nil:
		return fmt.Sprintf("[resource %s]", block.ResourceLink.Uri)
	case block.Image != nil:
		return "[image]"
	}
	return ""
}

// Raw re-encodes an SDK value as generic JSON for envelopes.
func Raw(value any) any {
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out any
	_ = json.Unmarshal(data, &out)
	return out
}

// ToolTitle summarizes a tool call update for progress output.
func ToolTitle(title string, kind acp.ToolKind, status *acp.ToolCallStatus) string {
	parts := []string{strings.TrimSpace(title)}
	if kind != "" {
		parts = append(parts, "("+string(kind)+")")
	}
	if status != nil {
		parts = append(parts, string(*status))
	}
	return strings.Join(parts, " ")
}

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

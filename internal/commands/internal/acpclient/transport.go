//go:build preview

// Package acpclient bridges newline-delimited ACP JSON-RPC to the remote ACP
// Streamable HTTP transport served by managed harness Deployments.
//
// Transport, as served by the harness image:
//   - initialize is a plain POST; the response carries Acp-Connection-Id.
//   - every later message is a POST answered with 202; responses and
//     notifications arrive on SSE streams opened with GET: one per connection
//     and one per session (Acp-Session-Id).
//   - DELETE closes the logical connection.
//
// The bridge exposes this as a single duplex stdio stream so standard ACP
// clients and the acp-go-sdk can talk to a remote harness unchanged.
package acpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

const (
	headerConnection = "Acp-Connection-Id"
	headerSession    = "Acp-Session-Id"
	headerToken      = "X-Access-Token"
	maxMessageBytes  = 16 << 20
	maxErrorBody     = 4 << 10
)

// Endpoint identifies one remote ACP entry.
type Endpoint struct {
	URL string
	// Token returns a currently valid access token for each request.
	Token func(context.Context) (string, error)
	// AffinityHeader is echoed from responses on later requests when set.
	AffinityHeader string
	// AffinityID seeds the affinity value to resume a known instance.
	AffinityID string
	// HTTP overrides the client; tests use it for loopback servers.
	HTTP *http.Client
}

// Bridge carries ACP messages between a local stream and the remote endpoint.
type Bridge struct {
	endpoint Endpoint
	http     *http.Client
	out      *lineWriter

	mu         sync.Mutex
	connection string
	affinity   string
	streams    map[string]context.CancelFunc
	// pendingNew maps outstanding session/new|load|resume request IDs, whose
	// results reveal the session ID that needs its own SSE stream.
	pendingNew map[string]bool
	wg         sync.WaitGroup
	failed     chan error
}

// NewBridge creates a bridge that writes inbound messages to out.
func NewBridge(endpoint Endpoint, out io.Writer) *Bridge {
	client := endpoint.HTTP
	if client == nil {
		client = &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()}
	}
	// Go forwards custom headers on redirects, which would leak the access
	// token to another host; the ACP entry never redirects legitimately.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Bridge{
		endpoint: endpoint, http: client, out: &lineWriter{w: out},
		affinity: endpoint.AffinityID, streams: map[string]context.CancelFunc{},
		pendingNew: map[string]bool{}, failed: make(chan error, 1),
	}
}

// Affinity returns the latest affinity value learned from the service.
func (b *Bridge) Affinity() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.affinity
}

// Run forwards newline-delimited JSON-RPC from in until in closes, ctx ends,
// or a remote stream fails. It always closes the remote connection.
func (b *Bridge) Run(ctx context.Context, in io.Reader) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	lines := make(chan []byte)
	readErr := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(in)
		scanner.Buffer(make([]byte, 64<<10), maxMessageBytes)
		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}
			select {
			case lines <- append([]byte(nil), line...):
			case <-ctx.Done():
				return
			}
		}
		readErr <- scanner.Err()
	}()
	defer b.close()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-b.failed:
			return err
		case err := <-readErr:
			return err
		case line := <-lines:
			if err := b.send(ctx, line); err != nil {
				return err
			}
		}
	}
}

type envelope struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

func (b *Bridge) send(ctx context.Context, line []byte) error {
	var message envelope
	if err := json.Unmarshal(line, &message); err != nil {
		return fmt.Errorf("invalid ACP message from client: %w", err)
	}
	if message.Method == "initialize" {
		return b.initialize(ctx, line)
	}
	if b.connectionID() == "" {
		return errors.New("ACP client sent a message before initialize")
	}
	session := sessionOf(message)
	switch message.Method {
	case "session/new":
		b.trackNew(message.ID)
	case "session/load", "session/resume":
		// Replayed history and the response flow on the session stream, which
		// must be open before the request is sent.
		b.ensureSessionStream(ctx, session)
		b.trackNew(message.ID)
	}
	if session != "" && message.Method != "session/load" && message.Method != "session/resume" {
		b.ensureSessionStream(ctx, session)
	}
	response, err := b.do(ctx, http.MethodPost, session, line, false)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusAccepted:
		return nil
	case response.StatusCode >= 200 && response.StatusCode < 300:
		// A direct JSON reply is also valid transport behavior.
		return b.forwardBody(response)
	default:
		return b.replyError(message.ID, httpError(message.Method, response))
	}
}

func (b *Bridge) initialize(ctx context.Context, line []byte) error {
	response, err := b.do(ctx, http.MethodPost, "", line, false)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return httpError("initialize", response)
	}
	connection := response.Header.Get(headerConnection)
	if connection == "" {
		return errors.New("ACP initialize response has no Acp-Connection-Id")
	}
	b.mu.Lock()
	b.connection = connection
	b.mu.Unlock()
	if err := b.forwardBody(response); err != nil {
		return err
	}
	return b.openStream(ctx, "")
}

func (b *Bridge) forwardBody(response *http.Response) error {
	data, err := io.ReadAll(io.LimitReader(response.Body, maxMessageBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxMessageBytes {
		return errors.New("ACP response exceeds the message size limit")
	}
	if data = bytes.TrimSpace(data); len(data) > 0 {
		b.deliver(data)
	}
	return nil
}

func (b *Bridge) trackNew(id json.RawMessage) {
	if len(id) == 0 {
		return
	}
	b.mu.Lock()
	b.pendingNew[string(id)] = true
	b.mu.Unlock()
}

// deliver writes one inbound message and opens a session stream when a new
// session ID first appears in a session/new result.
func (b *Bridge) deliver(data []byte) {
	var message envelope
	if json.Unmarshal(data, &message) == nil && message.Method == "" && len(message.ID) > 0 {
		b.mu.Lock()
		tracked := b.pendingNew[string(message.ID)]
		delete(b.pendingNew, string(message.ID))
		b.mu.Unlock()
		if tracked && len(message.Result) > 0 {
			var result struct {
				SessionID string `json:"sessionId"`
			}
			if json.Unmarshal(message.Result, &result) == nil && result.SessionID != "" {
				// Open before forwarding so the next prompt cannot race the stream.
				b.ensureSessionStream(context.Background(), result.SessionID)
			}
		}
	}
	b.out.writeLine(data)
}

func (b *Bridge) ensureSessionStream(ctx context.Context, session string) {
	if session == "" {
		return
	}
	b.mu.Lock()
	_, open := b.streams[session]
	b.mu.Unlock()
	if !open {
		if err := b.openStream(ctx, session); err != nil {
			b.fail(err)
		}
	}
}

// openStream starts an SSE reader and returns once the stream is established.
func (b *Bridge) openStream(ctx context.Context, session string) error {
	streamCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	b.mu.Lock()
	if _, exists := b.streams[session]; exists {
		b.mu.Unlock()
		cancel()
		return nil
	}
	b.streams[session] = cancel
	b.mu.Unlock()
	response, err := b.do(streamCtx, http.MethodGet, session, nil, true)
	if err != nil {
		cancel()
		return err
	}
	if response.StatusCode != http.StatusOK {
		defer func() { _ = response.Body.Close() }()
		cancel()
		return httpError("stream", response)
	}
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		defer func() { _ = response.Body.Close() }()
		err := readSSE(response.Body, b.deliver)
		if streamCtx.Err() == nil {
			if err == nil {
				err = errors.New("ACP event stream closed by the server")
			}
			b.fail(err)
		}
	}()
	return nil
}

func readSSE(body io.Reader, deliver func([]byte)) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), maxMessageBytes)
	var data bytes.Buffer
	for scanner.Scan() {
		line := scanner.Text()
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(value, " "))
			continue
		}
		if line == "" && data.Len() > 0 {
			deliver(append([]byte(nil), data.Bytes()...))
			data.Reset()
		}
	}
	return scanner.Err()
}

func (b *Bridge) do(ctx context.Context, method, session string, body []byte, stream bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, b.endpoint.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	token, err := b.endpoint.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire Deployment token: %w", err)
	}
	req.Header.Set(headerToken, token)
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
	}
	b.mu.Lock()
	if b.connection != "" {
		req.Header.Set(headerConnection, b.connection)
	}
	if b.endpoint.AffinityHeader != "" && b.affinity != "" {
		req.Header.Set(b.endpoint.AffinityHeader, b.affinity)
	}
	b.mu.Unlock()
	if session != "" {
		req.Header.Set(headerSession, session)
	}
	response, err := b.http.Do(req)
	if err != nil {
		return nil, err
	}
	if b.endpoint.AffinityHeader != "" {
		if value := response.Header.Get(b.endpoint.AffinityHeader); value != "" {
			b.mu.Lock()
			b.affinity = value
			b.mu.Unlock()
		}
	}
	return response, nil
}

func (b *Bridge) connectionID() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.connection
}

func (b *Bridge) fail(err error) {
	select {
	case b.failed <- err:
	default:
	}
}

// replyError answers a failed request locally so the client is not left
// waiting; notifications have no ID and fail the bridge instead.
func (b *Bridge) replyError(id json.RawMessage, err error) error {
	if len(id) == 0 {
		return err
	}
	reply, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32603, "message": err.Error()}})
	b.out.writeLine(reply)
	return nil
}

func (b *Bridge) close() {
	b.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(b.streams))
	for _, cancel := range b.streams {
		cancels = append(cancels, cancel)
	}
	connection := b.connection
	b.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	if connection != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if response, err := b.do(ctx, http.MethodDelete, "", nil, false); err == nil {
			_ = response.Body.Close()
		}
		cancel()
	}
	b.wg.Wait()
	b.http.CloseIdleConnections()
}

func sessionOf(message envelope) string {
	var params struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(message.Params, &params)
	return params.SessionID
}

// HTTPError is a non-success data-plane response. Platform gateway errors carry
// a JSON body with a code, message and request_id that explain the failure.
type HTTPError struct {
	Operation    string
	Status       int
	PlatformCode string
	Message      string
	RequestID    string
}

func (e *HTTPError) Error() string {
	message := e.Message
	if e.PlatformCode != "" {
		message = e.PlatformCode + ": " + message
	}
	return fmt.Sprintf("ACP %s: HTTP %d: %s", e.Operation, e.Status, message)
}

// CLIError classifies the response for structured CLI output.
func (e *HTTPError) CLIError(hint string) *output.CLIError {
	kind, code := output.KindNetwork, "ACP_HTTP_ERROR"
	switch e.Status {
	case http.StatusUnauthorized, http.StatusForbidden:
		kind, code = output.KindAuthOrPermission, "ACP_AUTH_FAILED"
		hint = "The Deployment token was rejected. Check AcquireDeploymentToken permission and the data-plane domain (--domain)."
	case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		code = "ACP_UNAVAILABLE"
	}
	details := map[string]any{"HTTPStatus": e.Status, "Operation": e.Operation}
	if e.PlatformCode != "" {
		details["PlatformCode"] = e.PlatformCode
	}
	if e.RequestID != "" {
		details["PlatformRequestId"] = e.RequestID
	}
	return output.NewCLIError(&output.Failure{Code: code, Kind: kind, Message: e.Error(), Hint: hint, Retryable: kind == output.KindNetwork, Details: details}).WithCause(e)
}

// httpError keeps only the status and a bounded, parsed body; the platform
// body never contains the request token.
func httpError(operation string, response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
	err := &HTTPError{Operation: operation, Status: response.StatusCode, Message: strings.TrimSpace(string(body))}
	var platform struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	}
	if json.Unmarshal(body, &platform) == nil && platform.Code != "" {
		err.PlatformCode, err.Message, err.RequestID = platform.Code, platform.Message, platform.RequestID
	}
	return err
}

type lineWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lineWriter) writeLine(data []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.w.Write(append(bytes.TrimSpace(data), '\n'))
}

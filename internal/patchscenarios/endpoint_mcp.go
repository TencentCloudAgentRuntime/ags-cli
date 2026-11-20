package patchscenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
)

const endpointMCPVersion = "2025-11-25"

var endpointMCPHTTP = http.Client{
	Timeout: 30 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

type endpointMCPSession struct {
	address, token, session, version string
	client                           *http.Client
	closed                           bool
}

func newEndpointMCPSession(address, token string) *endpointMCPSession {
	return &endpointMCPSession{address: address, token: token, client: &endpointMCPHTTP}
}

func (c *endpointMCPSession) open(ctx context.Context, registerCleanup func(func(context.Context) error)) error {
	status, err := c.initialize(ctx, registerCleanup)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("initialize metrics fixture MCP session returned HTTP %d", status)
	}
	_, status, err = c.post(ctx, 0, "notifications/initialized", nil)
	if err != nil {
		return err
	}
	if status != http.StatusAccepted {
		return errors.New("metrics fixture MCP initialization notification was rejected")
	}
	return nil
}

func (c *endpointMCPSession) initialize(ctx context.Context, registerCleanup func(func(context.Context) error)) (int, error) {
	resp, err := c.postRequest(ctx, 1, "initialize", map[string]any{
		"protocolVersion": endpointMCPVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "agr-preview-e2e", "version": "1"},
	})
	if err != nil {
		return 0, err
	}
	// Capture the session before decoding the response body or sending the
	// initialized notification. Both operations can fail after it was created.
	c.session = resp.Header.Get("Mcp-Session-Id")
	if c.session != "" {
		registerCleanup(c.close)
	}
	result, status, err := readEndpointMCPResponse(resp, 1, "initialize")
	if err != nil || status != http.StatusOK {
		return status, err
	}
	if c.session == "" || result.String("protocolVersion") != endpointMCPVersion {
		return status, errors.New("invalid Endpoint MCP session or protocol version")
	}
	c.version = endpointMCPVersion
	return status, nil
}

func (c *endpointMCPSession) close(ctx context.Context) error {
	if c.closed || c.session == "" {
		return nil
	}
	resp, err := c.request(ctx, http.MethodDelete, nil)
	if err != nil {
		return err
	}
	status := resp.StatusCode
	if err := resp.Body.Close(); err != nil {
		return errors.New("failed to close the MCP session deletion response body")
	}
	if status != http.StatusNoContent && status != http.StatusNotFound {
		return fmt.Errorf("delete Endpoint MCP session returned HTTP %d", status)
	}
	// Confirm the session is gone while the same bearer is still valid.
	// A 404 also permits retrying after a successful DELETE whose response
	// was lost. Authorization or transport failures cannot confirm cleanup.
	_, status, err = c.post(ctx, 1, "ping", nil)
	if err != nil {
		return err
	}
	if status != http.StatusNotFound {
		return fmt.Errorf("could not confirm Endpoint MCP session termination (HTTP %d)", status)
	}
	c.closed = true
	return nil
}

func (c *endpointMCPSession) invokeFixture(ctx context.Context, fixture endpointMetricFixture, count int) error {
	list, status, err := c.post(ctx, 2, "tools/list", nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("list metrics fixture MCP tools returned HTTP %d", status)
	}
	name := ""
	for _, candidate := range list.Objects("tools") {
		binding := candidate.Object("_meta").Object("com.tencentags/endpoint")
		if binding.String("upstreamToolName") == fixture.tool {
			name = candidate.String("name")
			break
		}
	}
	if name == "" {
		return errors.New("configured metrics fixture MCP tool was not discovered")
	}
	for i := range count {
		result, status, err := c.post(ctx, i+3, "tools/call", map[string]any{"name": name, "arguments": fixture.arguments})
		if err != nil {
			return err
		}
		if status != http.StatusOK || (result["isError"] != nil && result["isError"] != false) ||
			len(result.Objects("content")) == 0 {
			return errors.New("metrics fixture MCP tool did not return successful content")
		}
	}
	return nil
}

func (c *endpointMCPSession) post(ctx context.Context, id int, method string, params any) (apivalue.Object, int, error) {
	resp, err := c.postRequest(ctx, id, method, params)
	if err != nil {
		return nil, 0, err
	}
	return readEndpointMCPResponse(resp, id, method)
}

func (c *endpointMCPSession) postRequest(ctx context.Context, id int, method string, params any) (*http.Response, error) {
	payload, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id,omitempty"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{"2.0", id, method, params})
	if err != nil {
		return nil, errors.New("cannot encode Endpoint MCP request")
	}
	return c.request(ctx, http.MethodPost, bytes.NewReader(payload))
}

func (c *endpointMCPSession) request(ctx context.Context, method string, body io.Reader) (*http.Response, error) {
	address, err := url.Parse(c.address)
	if err != nil || address.Scheme != "https" || address.Hostname() == "" || address.User != nil {
		return nil, errors.New("invalid HTTPS MCP URL returned by the endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, method, address.String(), body)
	if err != nil {
		return nil, errors.New("cannot build Endpoint MCP request")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.token)
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
		// After an invalid initialize response, the session ID can still be
		// used for cleanup without asserting an unconfirmed protocol version.
		if c.version != "" {
			req.Header.Set("MCP-Protocol-Version", c.version)
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send Endpoint MCP %s request failed", method)
	}
	return resp, nil
}

func readEndpointMCPResponse(resp *http.Response, id int, method string) (apivalue.Object, int, error) {
	status := resp.StatusCode
	var envelope struct {
		JSONRPC string
		ID      int
		Result  apivalue.Object
		Error   *struct{ Code int }
	}
	var decodeErr error
	if status == http.StatusOK {
		decodeErr = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&envelope)
	}
	if err := resp.Body.Close(); err != nil {
		return nil, status, errors.New("failed to close the MCP response body")
	}
	if status == http.StatusOK &&
		(decodeErr != nil || envelope.JSONRPC != "2.0" || envelope.ID != id || envelope.Error != nil || envelope.Result == nil) {
		return nil, status, fmt.Errorf("invalid or unsuccessful Endpoint MCP %s response", method)
	}
	return envelope.Result, status, nil
}

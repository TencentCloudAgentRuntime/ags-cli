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

type endpointMCPSession struct{ address, token, session, version string }

func endpointMCPStatus(ctx context.Context, address, token string) (int, error) {
	client := endpointMCPSession{address: address, token: token}
	_, status, _, err := client.initialize(ctx)
	return status, err
}

func openEndpointMCPSession(ctx context.Context, address, token string) (endpointMCPSession, error) {
	client := endpointMCPSession{address: address, token: token}
	result, status, session, err := client.initialize(ctx)
	if err != nil {
		return client, err
	}
	if status != http.StatusOK || session == "" || result.String("protocolVersion") != endpointMCPVersion {
		return client, errors.New("metrics fixture failed to initialize an MCP session")
	}
	client.session, client.version = session, endpointMCPVersion
	_, status, _, err = client.post(ctx, 0, "notifications/initialized", nil)
	if err != nil {
		return client, err
	}
	if status != http.StatusAccepted {
		return client, errors.New("metrics fixture MCP initialization notification was rejected")
	}
	return client, nil
}

func (c endpointMCPSession) initialize(ctx context.Context) (apivalue.Object, int, string, error) {
	return c.post(ctx, 1, "initialize", map[string]any{
		"protocolVersion": endpointMCPVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "agr-preview-e2e", "version": "1"},
	})
}

func (c endpointMCPSession) invokeFixture(ctx context.Context, fixture endpointMetricFixture, count int) error {
	list, status, _, err := c.post(ctx, 2, "tools/list", nil)
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
		result, status, _, err := c.post(ctx, i+3, "tools/call", map[string]any{"name": name, "arguments": fixture.arguments})
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

func (c endpointMCPSession) post(ctx context.Context, id int, method string, params any) (apivalue.Object, int, string, error) {
	address, err := url.Parse(c.address)
	if err != nil || address.Scheme != "https" || address.Hostname() == "" || address.User != nil {
		return nil, 0, "", errors.New("invalid HTTPS MCP URL returned by the endpoint")
	}
	payload, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id,omitempty"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{"2.0", id, method, params})
	if err != nil {
		return nil, 0, "", errors.New("cannot encode Endpoint MCP request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, 0, "", errors.New("cannot build Endpoint MCP request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.token)
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
		req.Header.Set("MCP-Protocol-Version", c.version)
	}
	resp, err := endpointMCPHTTP.Do(req)
	if err != nil {
		return nil, 0, "", fmt.Errorf("send Endpoint MCP %s request failed", method)
	}
	var envelope struct {
		JSONRPC string
		ID      int
		Result  apivalue.Object
		Error   *struct{ Code int }
	}
	var decodeErr error
	if resp.StatusCode == http.StatusOK {
		decodeErr = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&envelope)
	}
	if err := resp.Body.Close(); err != nil {
		return nil, 0, "", errors.New("failed to close the MCP response body")
	}
	if resp.StatusCode == http.StatusOK &&
		(decodeErr != nil || envelope.JSONRPC != "2.0" || envelope.ID != id || envelope.Error != nil || envelope.Result == nil) {
		return nil, 0, "", fmt.Errorf("invalid or unsuccessful Endpoint MCP %s response", method)
	}
	return envelope.Result, resp.StatusCode, resp.Header.Get("Mcp-Session-Id"), nil
}

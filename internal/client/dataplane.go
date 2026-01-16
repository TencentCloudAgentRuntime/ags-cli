package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// DataPlaneClient handles data plane operations (code execution)
type DataPlaneClient struct {
	httpClient *http.Client
}

// NewDataPlaneClient creates a new data plane client
func NewDataPlaneClient(httpClient *http.Client) *DataPlaneClient {
	return &DataPlaneClient{
		httpClient: httpClient,
	}
}

// ExecuteCodeConfig contains configuration for code execution
type ExecuteCodeConfig struct {
	Domain      string // e.g., "49999-{instanceID}.{region}.{baseDomain}"
	AccessToken string // Access token for authentication
	Code        string
	Language    string
}

// ExecuteCode executes code via data plane HTTP API
func (c *DataPlaneClient) ExecuteCode(ctx context.Context, cfg *ExecuteCodeConfig, callbacks *StreamCallbacks) (*ExecuteResult, error) {
	url := fmt.Sprintf("https://%s/execute", cfg.Domain)

	// Default to python if not specified
	language := cfg.Language
	if language == "" {
		language = "python"
	}

	reqBody := map[string]any{
		"code":     cfg.Code,
		"language": language,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if cfg.AccessToken != "" {
		req.Header.Set("X-Access-Token", cfg.AccessToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to execute code: %s - %s", resp.Status, string(body))
	}

	// Parse streaming JSON Lines response
	return parseExecuteResponse(resp.Body, callbacks)
}

// parseExecuteResponse parses the streaming JSON Lines response from code execution
func parseExecuteResponse(body io.Reader, callbacks *StreamCallbacks) (*ExecuteResult, error) {
	result := &ExecuteResult{}
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		eventType, _ := event["type"].(string)
		switch eventType {
		case "stdout":
			if text, ok := event["text"].(string); ok {
				result.Stdout = append(result.Stdout, text)
				if callbacks != nil && callbacks.OnStdout != nil {
					callbacks.OnStdout(text)
				}
			}
		case "stderr":
			if text, ok := event["text"].(string); ok {
				result.Stderr = append(result.Stderr, text)
				if callbacks != nil && callbacks.OnStderr != nil {
					callbacks.OnStderr(text)
				}
			}
		case "result":
			result.Results = append(result.Results, event)
		case "error":
			result.Error = &ExecutionError{
				Name:      getString(event, "name"),
				Value:     getString(event, "value"),
				Traceback: getString(event, "traceback"),
			}
		}
	}

	return result, nil
}

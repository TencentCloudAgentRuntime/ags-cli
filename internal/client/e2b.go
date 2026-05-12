package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
)

// E2BControlPlane implements ControlPlaneClient for E2B API.
// Uses E2B REST API with API Key for control plane operations.
// Data plane operations are handled by ags-go-sdk via the cmd layer.
type E2BControlPlane struct {
	httpClient *http.Client
	apiKey     string
	domain     string
	region     string
}

// NewE2BControlPlane creates a new E2B control plane client
func NewE2BControlPlane() (*E2BControlPlane, error) {
	e2bCfg := config.GetE2BConfig()
	cfg := config.Get()
	httpClient := &http.Client{Timeout: 60 * time.Second}
	return &E2BControlPlane{
		httpClient: httpClient,
		apiKey:     e2bCfg.APIKey,
		domain:     cfg.Domain,
		region:     cfg.Region,
	}, nil
}

func (c *E2BControlPlane) getAPIEndpoint() string {
	return fmt.Sprintf("https://api.%s.%s", c.region, c.domain)
}

func (c *E2BControlPlane) doRequest(ctx context.Context, method, url string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	return c.httpClient.Do(req)
}

// ========== Tool Operations (not supported by E2B) ==========

// CreateTool is not supported by E2B backend
func (c *E2BControlPlane) CreateTool(ctx context.Context, opts *CreateToolOptions) (*Tool, error) {
	return nil, fmt.Errorf("tool operations are not supported by E2B backend, please use cloud backend")
}

// UpdateTool is not supported by E2B backend
func (c *E2BControlPlane) UpdateTool(ctx context.Context, opts *UpdateToolOptions) error {
	return fmt.Errorf("tool operations are not supported by E2B backend, please use cloud backend")
}

// DeleteTool is not supported by E2B backend
func (c *E2BControlPlane) DeleteTool(ctx context.Context, id string) error {
	return fmt.Errorf("tool operations are not supported by E2B backend, please use cloud backend")
}

// ListTools is not supported by E2B backend
func (c *E2BControlPlane) ListTools(ctx context.Context, opts *ListToolsOptions) (*ListToolsResult, error) {
	return nil, fmt.Errorf("tool operations are not supported by E2B backend, please use cloud backend")
}

// GetTool is not supported by E2B backend
func (c *E2BControlPlane) GetTool(ctx context.Context, id string) (*Tool, error) {
	return nil, fmt.Errorf("tool operations are not supported by E2B backend, please use cloud backend")
}

// ========== Instance Operations ==========

// CreateInstance creates a new sandbox instance.
// The returned Instance contains AccessToken which should be cached for data plane operations.
func (c *E2BControlPlane) CreateInstance(ctx context.Context, opts *CreateInstanceOptions) (*Instance, error) {
	url := c.getAPIEndpoint() + "/sandboxes"

	templateID := opts.ToolName
	if templateID == "" {
		templateID = opts.ToolID
	}
	if templateID == "" {
		templateID = "code-interpreter-v1"
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 300 // default 5 minutes
	}

	reqBody := map[string]any{
		"templateID": templateID,
		"timeout":    timeout,
	}

	// Translate the unified AuthMode enum into the E2B request fields:
	//   TOKEN   → secure=true,  network.allowPublicTraffic=false
	//   PUBLIC  → secure=true,  network.allowPublicTraffic=true
	//   NONE    → secure=false  (network is omitted; backend picks default)
	//   DEFAULT (or empty) → leave both fields unset (backend default)
	switch opts.AuthMode {
	case AuthModeToken:
		reqBody["secure"] = true
		reqBody["network"] = map[string]any{"allowPublicTraffic": false}
	case AuthModePublic:
		reqBody["secure"] = true
		reqBody["network"] = map[string]any{"allowPublicTraffic": true}
	case AuthModeNone:
		reqBody["secure"] = false
	case "", AuthModeDefault:
		// Use E2B backend default; do not set secure/network.
	default:
		return nil, fmt.Errorf("invalid auth mode %q for E2B backend", opts.AuthMode)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, url, reqBody)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, e2bHTTPError(resp)
	}

	var result struct {
		SandboxID       string `json:"sandboxID"`
		EnvdAccessToken string `json:"envdAccessToken"`
		Secure          *bool  `json:"secure,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &Instance{
		ID:          result.SandboxID,
		ToolID:      templateID,
		ToolName:    templateID,
		Status:      "running",
		CreatedAt:   time.Now().Format(time.RFC3339),
		AccessToken: result.EnvdAccessToken,
		Domain:      fmt.Sprintf("%s.%s", c.region, c.domain),
		Secure:      e2bSecure(result.Secure, result.EnvdAccessToken),
	}, nil
}

// e2bSecure derives Instance.Secure from an E2B sandbox payload.
// The explicit "secure" field wins when present; otherwise we fall back to
// inferring secure=(envdAccessToken != "") for backward compatibility with
// older backends that don't return the field.
func e2bSecure(secure *bool, envdAccessToken string) bool {
	if secure != nil {
		return *secure
	}
	return envdAccessToken != ""
}

// ListInstances returns all sandbox instances
func (c *E2BControlPlane) ListInstances(ctx context.Context, opts *ListInstancesOptions) (*ListInstancesResult, error) {
	url := c.getAPIEndpoint() + "/sandboxes"

	resp, err := c.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, e2bHTTPError(resp)
	}

	var sandboxes []struct {
		SandboxID  string `json:"sandboxID"`
		TemplateID string `json:"templateID"`
		Alias      string `json:"alias"`
		StartedAt  string `json:"startedAt"`
		Secure     *bool  `json:"secure,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sandboxes); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	instances := make([]Instance, len(sandboxes))
	for i, s := range sandboxes {
		// The list endpoint doesn't return envdAccessToken. If the backend
		// supplies "secure", use it; otherwise fall back to secure=true
		// (token required) so callers don't silently drop auth. Consumers
		// that need an authoritative value should fetch via GetInstance.
		secure := true
		if s.Secure != nil {
			secure = *s.Secure
		}
		instances[i] = Instance{
			ID:        s.SandboxID,
			ToolID:    s.TemplateID,
			ToolName:  s.TemplateID,
			Status:    "running",
			CreatedAt: s.StartedAt,
			Secure:    secure,
		}
	}

	return &ListInstancesResult{
		Instances:  instances,
		TotalCount: len(instances),
	}, nil
}

// GetInstance returns a specific instance by ID.
// Unlike ListInstances, this calls GET /sandboxes/{id} directly,
// which also returns the envdAccessToken field.
func (c *E2BControlPlane) GetInstance(ctx context.Context, id string) (*Instance, error) {
	url := c.getAPIEndpoint() + "/sandboxes/" + id

	resp, err := c.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, e2bHTTPError(resp)
	}

	var result struct {
		SandboxID       string `json:"sandboxID"`
		TemplateID      string `json:"templateID"`
		Alias           string `json:"alias"`
		StartedAt       string `json:"startedAt"`
		State           string `json:"state"`
		EnvdAccessToken string `json:"envdAccessToken"`
		Secure          *bool  `json:"secure,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &Instance{
		ID:          result.SandboxID,
		ToolID:      result.TemplateID,
		ToolName:    result.TemplateID,
		Status:      result.State,
		CreatedAt:   result.StartedAt,
		AccessToken: result.EnvdAccessToken,
		Domain:      fmt.Sprintf("%s.%s", c.region, c.domain),
		Secure:      e2bSecure(result.Secure, result.EnvdAccessToken),
	}, nil
}

// DeleteInstance deletes a sandbox instance
func (c *E2BControlPlane) DeleteInstance(ctx context.Context, id string) error {
	url := c.getAPIEndpoint() + "/sandboxes/" + id

	resp, err := c.doRequest(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return e2bHTTPError(resp)
	}

	return nil
}

// AcquireToken acquires an access token by calling GET /sandboxes/{id}.
// The envdAccessToken field is included in the instance detail response.
func (c *E2BControlPlane) AcquireToken(ctx context.Context, instanceID string) (string, error) {
	inst, err := c.GetInstance(ctx, instanceID)
	if err != nil {
		return "", fmt.Errorf("failed to acquire token: %w", err)
	}
	if inst.AccessToken == "" {
		return "", fmt.Errorf("no access token returned for instance %s", instanceID)
	}
	return inst.AccessToken, nil
}

func e2bHTTPError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	var payload struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Message != "" {
		if payload.Code != nil {
			return fmt.Errorf("E2B API returned %s: %s (code: %v)", resp.Status, payload.Message, payload.Code)
		}
		return fmt.Errorf("E2B API returned %s: %s", resp.Status, payload.Message)
	}
	if len(body) > 0 {
		return fmt.Errorf("E2B API returned %s: %s", resp.Status, string(body))
	}
	return fmt.Errorf("E2B API returned %s", resp.Status)
}

// ========== API Key Operations (not supported by E2B) ==========

// CreateAPIKey is not supported by E2B backend
func (c *E2BControlPlane) CreateAPIKey(ctx context.Context, name string) (*CreateAPIKeyResult, error) {
	return nil, fmt.Errorf("API key management is not supported by E2B backend")
}

// ListAPIKeys is not supported by E2B backend
func (c *E2BControlPlane) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	return nil, fmt.Errorf("API key management is not supported by E2B backend")
}

// DeleteAPIKey is not supported by E2B backend
func (c *E2BControlPlane) DeleteAPIKey(ctx context.Context, keyID string) error {
	return fmt.Errorf("API key management is not supported by E2B backend")
}

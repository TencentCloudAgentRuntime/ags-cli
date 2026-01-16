package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
)

// E2BClient implements APIClient for E2B API
type E2BClient struct {
	httpClient *http.Client
	apiKey     string
	domain     string
	region     string
	// tokenCache stores instanceID -> accessToken mapping
	tokenCache map[string]string
	tokenMu    sync.RWMutex
	dataPlane  *DataPlaneClient
}

// NewE2BClient creates a new E2B API client
func NewE2BClient() (*E2BClient, error) {
	cfg := config.GetE2BConfig()
	httpClient := &http.Client{Timeout: 60 * time.Second}
	return &E2BClient{
		httpClient: httpClient,
		apiKey:     cfg.APIKey,
		domain:     cfg.Domain,
		region:     cfg.Region,
		tokenCache: make(map[string]string),
		dataPlane:  NewDataPlaneClient(httpClient),
	}, nil
}

func (c *E2BClient) getAPIEndpoint() string {
	return fmt.Sprintf("https://api.%s.%s", c.region, c.domain)
}

func (c *E2BClient) doRequest(ctx context.Context, method, url string, body any) (*http.Response, error) {
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

// CreateTool is not supported by E2B backend
func (c *E2BClient) CreateTool(ctx context.Context, opts *CreateToolOptions) (*Tool, error) {
	return nil, fmt.Errorf("tool operations are not supported by E2B backend, please use cloud backend")
}

// UpdateTool is not supported by E2B backend
func (c *E2BClient) UpdateTool(ctx context.Context, opts *UpdateToolOptions) error {
	return fmt.Errorf("tool operations are not supported by E2B backend, please use cloud backend")
}

// DeleteTool is not supported by E2B backend
func (c *E2BClient) DeleteTool(ctx context.Context, id string) error {
	return fmt.Errorf("tool operations are not supported by E2B backend, please use cloud backend")
}

// ListTools is not supported by E2B backend
func (c *E2BClient) ListTools(ctx context.Context, opts *ListToolsOptions) (*ListToolsResult, error) {
	return nil, fmt.Errorf("tool operations are not supported by E2B backend, please use cloud backend")
}

// GetTool is not supported by E2B backend
func (c *E2BClient) GetTool(ctx context.Context, id string) (*Tool, error) {
	return nil, fmt.Errorf("tool operations are not supported by E2B backend, please use cloud backend")
}

// CreateInstance creates a new sandbox instance
func (c *E2BClient) CreateInstance(ctx context.Context, opts *CreateInstanceOptions) (*Instance, error) {
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

	resp, err := c.doRequest(ctx, http.MethodPost, url, reqBody)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to create instance: %s - %s", resp.Status, string(body))
	}

	var result struct {
		SandboxID       string `json:"sandboxID"`
		EnvdAccessToken string `json:"envdAccessToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Cache the access token for later use
	c.tokenMu.Lock()
	c.tokenCache[result.SandboxID] = result.EnvdAccessToken
	c.tokenMu.Unlock()

	return &Instance{
		ID:          result.SandboxID,
		ToolID:      templateID,
		ToolName:    templateID,
		Status:      "running",
		CreatedAt:   time.Now().Format(time.RFC3339),
		AccessToken: result.EnvdAccessToken,
		Domain:      fmt.Sprintf("%s.%s", c.region, c.domain),
	}, nil
}

// ListInstances returns all sandbox instances
func (c *E2BClient) ListInstances(ctx context.Context, opts *ListInstancesOptions) (*ListInstancesResult, error) {
	url := c.getAPIEndpoint() + "/sandboxes"

	resp, err := c.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list instances: %s - %s", resp.Status, string(body))
	}

	var sandboxes []struct {
		SandboxID  string `json:"sandboxID"`
		TemplateID string `json:"templateID"`
		Alias      string `json:"alias"`
		StartedAt  string `json:"startedAt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sandboxes); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	instances := make([]Instance, len(sandboxes))
	for i, s := range sandboxes {
		instances[i] = Instance{
			ID:        s.SandboxID,
			ToolID:    s.TemplateID,
			ToolName:  s.TemplateID,
			Status:    "running",
			CreatedAt: s.StartedAt,
		}
	}

	return &ListInstancesResult{
		Instances:  instances,
		TotalCount: len(instances),
	}, nil
}

// GetInstance returns a specific instance by ID
func (c *E2BClient) GetInstance(ctx context.Context, id string) (*Instance, error) {
	result, err := c.ListInstances(ctx, nil)
	if err != nil {
		return nil, err
	}
	for _, inst := range result.Instances {
		if inst.ID == id {
			return &inst, nil
		}
	}
	return nil, fmt.Errorf("instance not found: %s", id)
}

// DeleteInstance deletes a sandbox instance
func (c *E2BClient) DeleteInstance(ctx context.Context, id string) error {
	url := c.getAPIEndpoint() + "/sandboxes/" + id

	resp, err := c.doRequest(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete instance: %s - %s", resp.Status, string(body))
	}

	// Clean up cached token
	c.tokenMu.Lock()
	delete(c.tokenCache, id)
	c.tokenMu.Unlock()

	return nil
}

// Execute runs code in a sandbox instance
func (c *E2BClient) Execute(ctx context.Context, instanceID string, code string, language string) (*ExecuteResult, error) {
	return c.ExecuteStream(ctx, instanceID, code, language, nil)
}

// ExecuteStream runs code with streaming output callbacks
func (c *E2BClient) ExecuteStream(ctx context.Context, instanceID string, code string, language string, callbacks *StreamCallbacks) (*ExecuteResult, error) {
	// Get cached access token
	c.tokenMu.RLock()
	accessToken := c.tokenCache[instanceID]
	c.tokenMu.RUnlock()

	// Build sandbox domain: {port}-{instanceID}.{region}.{domain}
	sandboxDomain := fmt.Sprintf("%d-%s.%s.%s", 49999, instanceID, c.region, c.domain)

	// Execute code via data plane
	return c.dataPlane.ExecuteCode(ctx, &ExecuteCodeConfig{
		Domain:      sandboxDomain,
		AccessToken: accessToken,
		Code:        code,
		Language:    language,
	}, callbacks)
}

// ========== API Key Operations (not supported by E2B) ==========

// CreateAPIKey is not supported by E2B backend
func (c *E2BClient) CreateAPIKey(ctx context.Context, name string) (*CreateAPIKeyResult, error) {
	return nil, fmt.Errorf("API key management is not supported by E2B backend")
}

// ListAPIKeys is not supported by E2B backend
func (c *E2BClient) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	return nil, fmt.Errorf("API key management is not supported by E2B backend")
}

// DeleteAPIKey is not supported by E2B backend
func (c *E2BClient) DeleteAPIKey(ctx context.Context, keyID string) error {
	return fmt.Errorf("API key management is not supported by E2B backend")
}

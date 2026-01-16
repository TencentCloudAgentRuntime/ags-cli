package client

import (
	"context"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
)

// CloudClient implements APIClient for Tencent Cloud API
// Uses tencentcloud-sdk-go for Tool management (via CloudToolClient)
// Uses ags-go-sdk for Instance management and code execution (via CloudInstanceClient)
type CloudClient struct {
	tool     *CloudToolClient
	instance *CloudInstanceClient
	apikey   *CloudAPIKeyClient
}

// NewCloudClient creates a new Cloud API client
func NewCloudClient() (*CloudClient, error) {
	cfg := config.GetCloudConfig()

	// Create tool client (tencentcloud-sdk-go)
	toolClient, err := NewCloudToolClient(&cfg)
	if err != nil {
		return nil, err
	}

	// Create instance client (ags-go-sdk)
	instanceClient, err := NewCloudInstanceClient(&cfg)
	if err != nil {
		return nil, err
	}

	// Create API key client (tencentcloud-sdk-go)
	apikeyClient, err := NewCloudAPIKeyClient(&cfg)
	if err != nil {
		return nil, err
	}

	return &CloudClient{
		tool:     toolClient,
		instance: instanceClient,
		apikey:   apikeyClient,
	}, nil
}

// ========== Tool Operations (delegated to CloudToolClient) ==========

// CreateTool creates a new sandbox tool
func (c *CloudClient) CreateTool(ctx context.Context, opts *CreateToolOptions) (*Tool, error) {
	return c.tool.CreateTool(ctx, opts)
}

// UpdateTool updates a sandbox tool
func (c *CloudClient) UpdateTool(ctx context.Context, opts *UpdateToolOptions) error {
	return c.tool.UpdateTool(ctx, opts)
}

// ListTools returns available tools with optional filtering and pagination
func (c *CloudClient) ListTools(ctx context.Context, opts *ListToolsOptions) (*ListToolsResult, error) {
	return c.tool.ListTools(ctx, opts)
}

// GetTool returns a specific tool by ID
func (c *CloudClient) GetTool(ctx context.Context, id string) (*Tool, error) {
	return c.tool.GetTool(ctx, id)
}

// DeleteTool deletes a sandbox tool
func (c *CloudClient) DeleteTool(ctx context.Context, id string) error {
	return c.tool.DeleteTool(ctx, id)
}

// ========== Instance Operations (delegated to CloudInstanceClient) ==========

// CreateInstance creates a new sandbox instance
func (c *CloudClient) CreateInstance(ctx context.Context, opts *CreateInstanceOptions) (*Instance, error) {
	return c.instance.CreateInstance(ctx, opts)
}

// ListInstances returns sandbox instances with optional filters
func (c *CloudClient) ListInstances(ctx context.Context, opts *ListInstancesOptions) (*ListInstancesResult, error) {
	return c.instance.ListInstances(ctx, opts)
}

// GetInstance returns a specific instance by ID
func (c *CloudClient) GetInstance(ctx context.Context, id string) (*Instance, error) {
	return c.instance.GetInstance(ctx, id)
}

// DeleteInstance deletes a sandbox instance
func (c *CloudClient) DeleteInstance(ctx context.Context, id string) error {
	return c.instance.DeleteInstance(ctx, id)
}

// Execute runs code in a sandbox instance
func (c *CloudClient) Execute(ctx context.Context, instanceID string, code string, language string) (*ExecuteResult, error) {
	return c.instance.Execute(ctx, instanceID, code, language)
}

// ExecuteStream runs code with streaming output callbacks
func (c *CloudClient) ExecuteStream(ctx context.Context, instanceID string, code string, language string, callbacks *StreamCallbacks) (*ExecuteResult, error) {
	return c.instance.ExecuteStream(ctx, instanceID, code, language, callbacks)
}

// ========== API Key Operations (delegated to CloudAPIKeyClient) ==========

// CreateAPIKey creates a new API key
func (c *CloudClient) CreateAPIKey(ctx context.Context, name string) (*CreateAPIKeyResult, error) {
	return c.apikey.CreateAPIKey(ctx, name)
}

// ListAPIKeys returns all API keys
func (c *CloudClient) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	return c.apikey.ListAPIKeys(ctx)
}

// DeleteAPIKey deletes an API key
func (c *CloudClient) DeleteAPIKey(ctx context.Context, keyID string) error {
	return c.apikey.DeleteAPIKey(ctx, keyID)
}

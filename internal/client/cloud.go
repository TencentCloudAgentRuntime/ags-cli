package client

import (
	"context"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
)

// CloudControlPlane implements ControlPlaneClient for Tencent Cloud API.
// Uses tencentcloud-sdk-go for all control plane operations:
//   - Tool management via CloudToolClient
//   - Instance management via CloudInstanceClient
//   - API Key management via CloudAPIKeyClient
type CloudControlPlane struct {
	tool     *CloudToolClient
	instance *CloudInstanceClient
	apikey   *CloudAPIKeyClient
}

// NewCloudControlPlane creates a new Cloud control plane client
func NewCloudControlPlane() (*CloudControlPlane, error) {
	cfg := config.Get()
	cloudCfg := config.GetCloudConfig()

	// Create tool client (tencentcloud-sdk-go)
	toolClient, err := NewCloudToolClient(cfg, &cloudCfg)
	if err != nil {
		return nil, err
	}

	// Create instance client (tencentcloud-sdk-go)
	instanceClient, err := NewCloudInstanceClient(cfg, &cloudCfg)
	if err != nil {
		return nil, err
	}

	// Create API key client (tencentcloud-sdk-go)
	apikeyClient, err := NewCloudAPIKeyClient(cfg, &cloudCfg)
	if err != nil {
		return nil, err
	}

	return &CloudControlPlane{
		tool:     toolClient,
		instance: instanceClient,
		apikey:   apikeyClient,
	}, nil
}

// ========== Tool Operations (delegated to CloudToolClient) ==========

// CreateTool creates a new sandbox tool
func (c *CloudControlPlane) CreateTool(ctx context.Context, opts *CreateToolOptions) (*Tool, error) {
	return c.tool.CreateTool(ctx, opts)
}

// UpdateTool updates a sandbox tool
func (c *CloudControlPlane) UpdateTool(ctx context.Context, opts *UpdateToolOptions) error {
	return c.tool.UpdateTool(ctx, opts)
}

// ListTools returns available tools with optional filtering and pagination
func (c *CloudControlPlane) ListTools(ctx context.Context, opts *ListToolsOptions) (*ListToolsResult, error) {
	return c.tool.ListTools(ctx, opts)
}

// GetTool returns a specific tool by ID
func (c *CloudControlPlane) GetTool(ctx context.Context, id string) (*Tool, error) {
	return c.tool.GetTool(ctx, id)
}

// DeleteTool deletes a sandbox tool
func (c *CloudControlPlane) DeleteTool(ctx context.Context, id string) error {
	return c.tool.DeleteTool(ctx, id)
}

// ========== Instance Operations (delegated to CloudInstanceClient) ==========

// CreateInstance creates a new sandbox instance
func (c *CloudControlPlane) CreateInstance(ctx context.Context, opts *CreateInstanceOptions) (*Instance, error) {
	return c.instance.CreateInstance(ctx, opts)
}

// ListInstances returns sandbox instances with optional filters
func (c *CloudControlPlane) ListInstances(ctx context.Context, opts *ListInstancesOptions) (*ListInstancesResult, error) {
	return c.instance.ListInstances(ctx, opts)
}

// GetInstance returns a specific instance by ID
func (c *CloudControlPlane) GetInstance(ctx context.Context, id string) (*Instance, error) {
	return c.instance.GetInstance(ctx, id)
}

// DeleteInstance deletes a sandbox instance
func (c *CloudControlPlane) DeleteInstance(ctx context.Context, id string) error {
	return c.instance.DeleteInstance(ctx, id)
}

// AcquireToken acquires an access token for data plane operations
func (c *CloudControlPlane) AcquireToken(ctx context.Context, instanceID string) (string, error) {
	return c.instance.AcquireToken(ctx, instanceID)
}

// ========== API Key Operations (delegated to CloudAPIKeyClient) ==========

// CreateAPIKey creates a new API key
func (c *CloudControlPlane) CreateAPIKey(ctx context.Context, name string) (*CreateAPIKeyResult, error) {
	return c.apikey.CreateAPIKey(ctx, name)
}

// ListAPIKeys returns all API keys
func (c *CloudControlPlane) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	return c.apikey.ListAPIKeys(ctx)
}

// DeleteAPIKey deletes an API key
func (c *CloudControlPlane) DeleteAPIKey(ctx context.Context, keyID string) error {
	return c.apikey.DeleteAPIKey(ctx, keyID)
}

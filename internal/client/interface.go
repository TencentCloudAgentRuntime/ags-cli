package client

import "context"

// ControlPlaneClient defines the interface for control plane operations.
// Control plane handles instance lifecycle management, tool management, and API key management.
//
// There are two backend implementations:
//   - Cloud backend: Uses Tencent Cloud API with AKSK credentials (tencentcloud-sdk-go)
//   - E2B backend: Uses E2B protocol with API Key (REST API)
//
// Data plane operations (code execution, file operations, etc.) are handled separately
// via ags-go-sdk, which uses E2B protocol with Access Token.
type ControlPlaneClient interface {
	// Tool operations (cloud backend only, E2B backend returns not supported error)
	CreateTool(ctx context.Context, opts *CreateToolOptions) (*Tool, error)
	UpdateTool(ctx context.Context, opts *UpdateToolOptions) error
	ListTools(ctx context.Context, opts *ListToolsOptions) (*ListToolsResult, error)
	GetTool(ctx context.Context, id string) (*Tool, error)
	DeleteTool(ctx context.Context, id string) error

	// Instance operations (both backends supported)
	CreateInstance(ctx context.Context, opts *CreateInstanceOptions) (*Instance, error)
	ListInstances(ctx context.Context, opts *ListInstancesOptions) (*ListInstancesResult, error)
	GetInstance(ctx context.Context, id string) (*Instance, error)
	DeleteInstance(ctx context.Context, id string) error

	// AcquireToken acquires an access token for data plane operations.
	// For cloud backend, this calls AcquireSandboxInstanceToken API.
	// For E2B backend, this calls the connect API with a minimal timeout.
	AcquireToken(ctx context.Context, instanceID string) (string, error)

	// API Key operations (cloud backend only, E2B backend returns not supported error)
	CreateAPIKey(ctx context.Context, name string) (*CreateAPIKeyResult, error)
	ListAPIKeys(ctx context.Context) ([]APIKey, error)
	DeleteAPIKey(ctx context.Context, keyID string) error
}

// NewControlPlaneClient creates a new control plane client based on the backend type.
// Supported backends: "e2b", "cloud"
func NewControlPlaneClient(backend string) (ControlPlaneClient, error) {
	switch backend {
	case "e2b":
		return NewE2BControlPlane()
	case "cloud":
		return NewCloudControlPlane()
	default:
		return NewE2BControlPlane()
	}
}

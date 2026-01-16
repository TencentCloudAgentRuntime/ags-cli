package client

import "context"

// APIClient defines the interface for interacting with AGS API
type APIClient interface {
	// Tool operations
	CreateTool(ctx context.Context, opts *CreateToolOptions) (*Tool, error)
	UpdateTool(ctx context.Context, opts *UpdateToolOptions) error
	ListTools(ctx context.Context, opts *ListToolsOptions) (*ListToolsResult, error)
	GetTool(ctx context.Context, id string) (*Tool, error)
	DeleteTool(ctx context.Context, id string) error

	// Instance operations
	CreateInstance(ctx context.Context, opts *CreateInstanceOptions) (*Instance, error)
	ListInstances(ctx context.Context, opts *ListInstancesOptions) (*ListInstancesResult, error)
	GetInstance(ctx context.Context, id string) (*Instance, error)
	DeleteInstance(ctx context.Context, id string) error

	// Code execution
	Execute(ctx context.Context, instanceID string, code string, language string) (*ExecuteResult, error)
	ExecuteStream(ctx context.Context, instanceID string, code string, language string, callbacks *StreamCallbacks) (*ExecuteResult, error)

	// API Key operations
	CreateAPIKey(ctx context.Context, name string) (*CreateAPIKeyResult, error)
	ListAPIKeys(ctx context.Context) ([]APIKey, error)
	DeleteAPIKey(ctx context.Context, keyID string) error
}

// NewClient creates a new API client based on the backend type
func NewClient(backend string) (APIClient, error) {
	switch backend {
	case "e2b":
		return NewE2BClient()
	case "cloud":
		return NewCloudClient()
	default:
		return NewE2BClient()
	}
}

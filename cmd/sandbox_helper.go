package cmd

import (
	"context"
	"fmt"

	"github.com/TencentCloudAgentRuntime/ags-go-sdk/connection"
	"github.com/TencentCloudAgentRuntime/ags-go-sdk/constant"
	"github.com/TencentCloudAgentRuntime/ags-go-sdk/sandbox/code"
	"github.com/TencentCloudAgentRuntime/ags-go-sdk/sandbox/core"
	toolcode "github.com/TencentCloudAgentRuntime/ags-go-sdk/tool/code"
	"github.com/TencentCloudAgentRuntime/ags-go-sdk/tool/command"
	"github.com/TencentCloudAgentRuntime/ags-go-sdk/tool/filesystem"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/token"
)

// ConnectWithToken connects to a sandbox instance using a cached access token.
// This function bypasses the control plane API and directly constructs data plane clients.
//
// Use this function when:
//   - You have a cached access token from a previous instance creation
//   - You want to avoid control plane API calls (which require AKSK for cloud backend)
//   - You only need data plane operations (Files, Commands, Code)
//
// Note: The returned sandbox has limited functionality:
//   - Kill(), SetTimeoutSeconds(), GetInfo() will NOT work (require control plane access)
//   - Files, Commands, Code clients work normally
//
// Parameters:
//   - ctx: Context for the operation
//   - instanceID: The sandbox instance ID
//   - accessToken: The access token for data plane authentication
//
// Returns:
//   - *code.Sandbox: A sandbox instance with data plane clients initialized
//   - error: Any error encountered during initialization
func ConnectWithToken(ctx context.Context, instanceID string, accessToken string) (*code.Sandbox, error) {
	cloudCfg := config.GetCloudConfig()

	// Determine the data plane domain
	var domain string
	if cloudCfg.Internal {
		domain = cloudCfg.DataPlaneDomain()
	} else {
		domain = fmt.Sprintf("%s.tencentags.com", cloudCfg.Region)
	}

	// Create connection config
	connConfig := &connection.Config{
		Domain:      domain,
		AccessToken: accessToken,
	}

	// Create core with nil client (we only use data plane operations)
	coreInstance := core.NewCore(nil, instanceID, connConfig)

	// Create sandbox wrapper
	sandbox := &code.Sandbox{
		Core: coreInstance,
	}

	// Initialize data plane clients
	var err error

	// Initialize filesystem client
	sandbox.Files, err = filesystem.New(&connection.Config{
		Domain:      sandbox.GetHost(constant.EnvdPort),
		AccessToken: accessToken,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize filesystem client: %w", err)
	}

	// Initialize command client
	sandbox.Commands, err = command.New(&connection.Config{
		Domain:      sandbox.GetHost(constant.EnvdPort),
		AccessToken: accessToken,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize command client: %w", err)
	}

	// Initialize code execution client
	sandbox.Code = toolcode.New(&connection.Config{
		Domain:      sandbox.GetHost(constant.CodePort),
		AccessToken: accessToken,
	})

	return sandbox, nil
}

// GetCachedTokenOrAcquire gets the access token from cache, or acquires a new one if not cached.
// If not cached, it will call the control plane API to acquire a new token.
// For Cloud backend, this calls AcquireSandboxInstanceToken API.
// For E2B backend, this calls the connect API with a minimal timeout.
//
// Parameters:
//   - ctx: Context for the operation
//   - instanceID: The sandbox instance ID
//
// Returns:
//   - string: The access token
//   - error: Any error encountered
func GetCachedTokenOrAcquire(ctx context.Context, instanceID string) (string, error) {
	// Try to get from cache first
	tokenCache, err := token.NewCache()
	if err != nil {
		return "", fmt.Errorf("failed to create token cache: %w", err)
	}

	if cachedToken, found := tokenCache.Get(instanceID); found {
		return cachedToken, nil
	}

	// Token not in cache, acquire from control plane API
	accessToken, err := acquireInstanceToken(ctx, instanceID)
	if err != nil {
		return "", err
	}

	// Cache the newly acquired token
	if err := tokenCache.Set(instanceID, accessToken); err != nil {
		// Log warning but don't fail
		// The operation can still succeed without caching
	}

	return accessToken, nil
}

// ConnectSandboxWithCache connects to a sandbox using cached token, falling back to SDK Connect if needed.
// This is the recommended way to connect to an existing sandbox instance.
//
// Parameters:
//   - ctx: Context for the operation
//   - instanceID: The sandbox instance ID
//
// Returns:
//   - *code.Sandbox: A sandbox instance ready for data plane operations
//   - error: Any error encountered
func ConnectSandboxWithCache(ctx context.Context, instanceID string) (*code.Sandbox, error) {
	accessToken, err := GetCachedTokenOrAcquire(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	return ConnectWithToken(ctx, instanceID, accessToken)
}

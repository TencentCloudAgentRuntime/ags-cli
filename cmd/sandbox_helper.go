package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-go-sdk/connection"
	"github.com/TencentCloudAgentRuntime/ags-go-sdk/constant"
	"github.com/TencentCloudAgentRuntime/ags-go-sdk/sandbox/code"
	"github.com/TencentCloudAgentRuntime/ags-go-sdk/sandbox/core"
	toolcode "github.com/TencentCloudAgentRuntime/ags-go-sdk/tool/code"
	"github.com/TencentCloudAgentRuntime/ags-go-sdk/tool/command"
	"github.com/TencentCloudAgentRuntime/ags-go-sdk/tool/filesystem"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/client"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/token"
)

// resolveUser returns the effective sandbox user.
// Priority: flag value > config default_user > "user".
func resolveUser(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if cfgUser := config.GetSandboxUser(); cfgUser != "" {
		return cfgUser
	}
	return "user"
}

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
	cfg := config.Get()

	// Determine the data plane domain
	domain := cfg.DataPlaneRegionDomain()

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
// For E2B backend, the token must exist in cache (acquired during instance creation).
// For Cloud backend, if not cached, it will call the control plane API to acquire a new token.
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

	// Token not in cache - acquire from control plane API
	// Cloud backend: acquire token via AcquireSandboxInstanceToken API
	// E2B backend: acquire token via GET /sandboxes/{id}
	accessToken, err := acquireInstanceToken(ctx, instanceID)
	if err != nil {
		return "", err
	}

	// Cache the newly acquired token (best-effort, ignore errors)
	_ = tokenCache.Set(instanceID, accessToken)

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

// GetOrCreateSandboxForDataPlane returns an existing sandbox or creates one via
// the configured control-plane backend. It keeps run/exec/file aligned with
// `ags instance create` instead of bypassing config through the cloud SDK.
func GetOrCreateSandboxForDataPlane(ctx context.Context, instanceID, tool string, keepAlive bool) (*code.Sandbox, func(), time.Duration, error) {
	if instanceID != "" {
		sandbox, err := ConnectSandboxWithCache(ctx, instanceID)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("failed to connect to instance %s: %w", instanceID, err)
		}
		return sandbox, func() {}, 0, nil
	}

	createStart := time.Now()
	apiClient, err := client.NewControlPlaneClient(config.GetBackend())
	if err != nil {
		return nil, nil, 0, fmt.Errorf("failed to create API client: %w", err)
	}

	if tool == "" {
		tool = "code-interpreter-v1"
	}
	instance, err := apiClient.CreateInstance(ctx, &client.CreateInstanceOptions{
		ToolName: tool,
		Timeout:  300,
	})
	createDuration := time.Since(createStart)
	if err != nil {
		return nil, nil, createDuration, fmt.Errorf("failed to create instance: %w", err)
	}

	accessToken, err := accessTokenForInstance(ctx, apiClient, instance)
	if err != nil {
		_ = apiClient.DeleteInstance(ctx, instance.ID)
		return nil, nil, createDuration, err
	}

	sandbox, err := ConnectWithToken(ctx, instance.ID, accessToken)
	if err != nil {
		_ = apiClient.DeleteInstance(ctx, instance.ID)
		return nil, nil, createDuration, fmt.Errorf("failed to connect to created instance %s: %w", instance.ID, err)
	}

	if keepAlive {
		output.PrintInfo(fmt.Sprintf("Created instance: %s (kept alive)", instance.ID))
		return sandbox, func() {}, createDuration, nil
	}

	cleanup := func() {
		_ = apiClient.DeleteInstance(ctx, instance.ID)
		if tokenCache, err := token.NewCache(); err == nil {
			_ = tokenCache.Delete(instance.ID)
		}
	}

	return sandbox, cleanup, createDuration, nil
}

func accessTokenForInstance(ctx context.Context, apiClient client.ControlPlaneClient, instance *client.Instance) (string, error) {
	if instance == nil {
		return "", fmt.Errorf("created instance is nil")
	}

	var accessToken string
	var err error
	if instance.AccessToken != "" {
		accessToken = instance.AccessToken
	} else if instance.Secure {
		accessToken, err = apiClient.AcquireToken(ctx, instance.ID)
		if err != nil {
			return "", fmt.Errorf("failed to acquire token: %w", err)
		}
	}

	if accessToken != "" {
		if tokenCache, cacheErr := token.NewCache(); cacheErr == nil {
			_ = tokenCache.Set(instance.ID, accessToken)
		}
	}

	return accessToken, nil
}

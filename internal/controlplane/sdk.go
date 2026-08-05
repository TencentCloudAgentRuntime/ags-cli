// Package controlplane adapts normalized command requests to TencentCloud AGS
// control-plane API calls.
package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	requestio "github.com/TencentCloudAgentRuntime/ags-cli/internal/cli/request"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/client"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/dataplane/token"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

// SDK adapts typed TencentCloud SDK operations to command dependency interfaces.
type SDK struct {
	Client                      *ags.Client
	NewClient                   func() (*ags.Client, error)
	StartSandboxInstance        func(context.Context, *ags.Client, *ags.StartSandboxInstanceRequest) (*ags.StartSandboxInstanceResponseParams, error)
	AcquireSandboxInstanceToken func(context.Context, *ags.Client, *ags.AcquireSandboxInstanceTokenRequest) (*ags.AcquireSandboxInstanceTokenResponseParams, error)
	TokenCache                  *token.Cache
	TokenCacheReady             bool
	Warnf                       func(format string, args ...any)
	// RawSender, when set, is used by the default-case fallback to send raw
	// HTTP for actions not yet covered by typed SDK wrappers (e.g. identity/
	// credential modules in workflow-adapter mode). When nil the fallback
	// builds its own cloudapi.Caller from config. Injecting a sender makes the
	// fallback path unit-testable without a live network.
	RawSender RawAPISender
}

type jsonRequest interface {
	FromJsonString(string) error
}

// Call executes a generated API action using a map-based request payload.
func (s *SDK) Call(ctx context.Context, action string, request map[string]any) (any, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	apiClient, err := s.cloudClient()
	if err != nil {
		return nil, err
	}
	switch action {
	case "CreateAPIKey":
		req := ags.NewCreateAPIKeyRequest()
		if err := fillRequest("apikey.create", request, req); err != nil {
			return nil, err
		}
		return callCreateAPIKey(ctx, apiClient, req)
	case "DescribeAPIKeyList":
		req := ags.NewDescribeAPIKeyListRequest()
		if err := fillRequest("apikey.list", request, req); err != nil {
			return nil, err
		}
		return callDescribeAPIKeyList(ctx, apiClient, req)
	case "DeleteAPIKey":
		req := ags.NewDeleteAPIKeyRequest()
		if err := fillRequest("apikey.delete", request, req); err != nil {
			return nil, err
		}
		return callDeleteAPIKey(ctx, apiClient, req)
	case "CreateSandboxTool":
		req := ags.NewCreateSandboxToolRequest()
		if err := fillRequest("tool.create", request, req); err != nil {
			return nil, err
		}
		return callCreateSandboxTool(ctx, apiClient, req)
	case "DescribeSandboxToolList":
		req := ags.NewDescribeSandboxToolListRequest()
		if err := fillRequest("tool.list", request, req); err != nil {
			return nil, err
		}
		return callDescribeSandboxToolList(ctx, apiClient, req)
	case "UpdateSandboxTool":
		req := ags.NewUpdateSandboxToolRequest()
		if err := fillRequest("tool.update", request, req); err != nil {
			return nil, err
		}
		return callUpdateSandboxTool(ctx, apiClient, req)
	case "StartSandboxInstance":
		req := ags.NewStartSandboxInstanceRequest()
		if err := fillRequest("instance.create", request, req); err != nil {
			return nil, err
		}
		resp, err := s.startSandboxInstance(ctx, apiClient, req)
		if err != nil {
			return nil, fmt.Errorf("failed to create instance: %w", err)
		}
		if resp.Instance == nil {
			return nil, fmt.Errorf("no instance returned from API")
		}
		if err := s.cacheInstanceToken(ctx, apiClient, resp.Instance); err != nil {
			s.warnf("Warning: Failed to cache access token: %v\n", err)
		}
		return resp, nil
	case "DescribeSandboxInstanceList":
		req := ags.NewDescribeSandboxInstanceListRequest()
		if err := fillRequest("instance.list", request, req); err != nil {
			return nil, err
		}
		result, err := callDescribeSandboxInstanceList(ctx, apiClient, req)
		if err != nil {
			return nil, fmt.Errorf("failed to list instances: %w", err)
		}
		return result, nil
	case "UpdateSandboxInstance":
		req := ags.NewUpdateSandboxInstanceRequest()
		if err := fillRequest("instance.update", request, req); err != nil {
			return nil, err
		}
		return callUpdateSandboxInstance(ctx, apiClient, req)
	case "PauseSandboxInstance":
		req := ags.NewPauseSandboxInstanceRequest()
		if err := fillRequest("instance.pause", request, req); err != nil {
			return nil, err
		}
		return callPauseSandboxInstance(ctx, apiClient, req)
	case "ResumeSandboxInstance":
		req := ags.NewResumeSandboxInstanceRequest()
		if err := fillRequest("instance.resume", request, req); err != nil {
			return nil, err
		}
		return callResumeSandboxInstance(ctx, apiClient, req)
	case "CreatePreCacheImageTask":
		req := ags.NewCreatePreCacheImageTaskRequest()
		if err := fillRequest("pre-cache-image-task.create", request, req); err != nil {
			return nil, err
		}
		return callCreatePreCacheImageTask(ctx, apiClient, req)
	case "DescribePreCacheImageTask":
		req := ags.NewDescribePreCacheImageTaskRequest()
		if err := fillRequest("pre-cache-image-task.get", request, req); err != nil {
			return nil, err
		}
		return callDescribePreCacheImageTask(ctx, apiClient, req)
	default:
		// Fallback: for Actions not yet in the typed SDK (e.g. identity/credential
		// modules added via workflow adapter before SDK sync), send as raw HTTP
		// using the same path as `agr api call`. This ensures commands are
		// functional immediately without waiting for SDK updates.
		raw, err := json.Marshal(request)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request for %s: %w", action, err)
		}
		result, err := RawAPIClient{Sender: s.RawSender}.RawCall(ctx, action, raw)
		if err != nil {
			// Classify TencentCloud SDK errors into typed CLIErrors so callers
			// see the real API code (e.g. AuthFailure, ResourceNotFound) and
			// kind/retryable flags, mirroring the typed call wrappers below.
			return nil, client.ClassifyCloudError(err)
		}
		// Extract the inner Response object for consistency with typed calls.
		if respMap, ok := result.Response.(map[string]any); ok {
			if inner, ok := respMap["Response"].(map[string]any); ok {
				// Remove RequestId from the data payload (it's metadata).
				delete(inner, "RequestId")
				return inner, nil
			}
		}
		return result.Response, nil
	}
}

// DeleteTool deletes a sandbox tool by ID.
func (s *SDK) DeleteTool(ctx context.Context, toolID string) error {
	client, err := s.cloudClient()
	if err != nil {
		return err
	}
	req := ags.NewDeleteSandboxToolRequest()
	req.ToolId = &toolID
	_, err = callDeleteSandboxTool(ctx, client, req)
	return err
}

// GetTool returns a sandbox tool by ID or a structured not-found error.
func (s *SDK) GetTool(ctx context.Context, toolID string) (*ags.SandboxTool, error) {
	client, err := s.cloudClient()
	if err != nil {
		return nil, err
	}
	req := ags.NewDescribeSandboxToolListRequest()
	req.ToolIds = []*string{&toolID}
	resp, err := callDescribeSandboxToolList(ctx, client, req)
	if err != nil {
		return nil, err
	}
	if len(resp.SandboxToolSet) == 0 {
		return nil, output.NewNotFoundError("TOOL_NOT_FOUND", fmt.Sprintf("tool not found: %s", toolID), "Run 'agr tool list' to find available tools.")
	}
	return resp.SandboxToolSet[0], nil
}

// DeleteInstance stops a sandbox instance and removes its cached token.
func (s *SDK) DeleteInstance(ctx context.Context, instanceID string) error {
	client, err := s.cloudClient()
	if err != nil {
		return err
	}
	req := ags.NewStopSandboxInstanceRequest()
	req.InstanceId = &instanceID
	if _, err := callStopSandboxInstance(ctx, client, req); err != nil {
		return err
	}
	if cache := s.cache(); cache != nil {
		_ = cache.Delete(instanceID)
	}
	return nil
}

// GetInstance returns a sandbox instance by ID or a structured not-found error.
func (s *SDK) GetInstance(ctx context.Context, instanceID string) (*ags.SandboxInstance, error) {
	client, err := s.cloudClient()
	if err != nil {
		return nil, err
	}
	req := ags.NewDescribeSandboxInstanceListRequest()
	req.InstanceIds = []*string{&instanceID}
	resp, err := callDescribeSandboxInstanceList(ctx, client, req)
	if err != nil {
		return nil, err
	}
	if len(resp.InstanceSet) == 0 {
		return nil, output.NewNotFoundError("INSTANCE_NOT_FOUND", fmt.Sprintf("instance not found: %s", instanceID), "Run 'agr instance list' to find active instances.")
	}
	return resp.InstanceSet[0], nil
}

// IsNotFound reports whether err represents a structured not-found failure.
func (s *SDK) IsNotFound(err error) bool {
	var cliErr *output.CLIError
	if errors.As(err, &cliErr) {
		return cliErr.Failure != nil && cliErr.Failure.Kind == output.KindNotFound
	}
	return false
}

func (s *SDK) cloudClient() (*ags.Client, error) {
	if s.Client != nil {
		return s.Client, nil
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	newClient := s.NewClient
	if newClient == nil {
		newClient = client.NewCloudClient
	}
	apiClient, err := newClient()
	if err != nil {
		return nil, err
	}
	s.Client = apiClient
	return apiClient, nil
}

func (s *SDK) cache() *token.Cache {
	if s.TokenCacheReady {
		return s.TokenCache
	}
	s.TokenCacheReady = true
	cache, err := token.NewCache()
	if err != nil {
		s.warnf("Warning: Failed to initialize token cache: %v\n", err)
		return nil
	}
	s.TokenCache = cache
	return s.TokenCache
}

func (s *SDK) cacheInstanceToken(ctx context.Context, apiClient *ags.Client, instance *ags.SandboxInstance) error {
	if instance.AuthMode != nil && *instance.AuthMode == "NONE" {
		return nil
	}
	if instance.InstanceId == nil || *instance.InstanceId == "" {
		return nil
	}
	cache := s.cache()
	if cache == nil {
		return nil
	}
	req := ags.NewAcquireSandboxInstanceTokenRequest()
	req.InstanceId = instance.InstanceId
	resp, err := s.acquireSandboxInstanceToken(ctx, apiClient, req)
	if err != nil {
		return fmt.Errorf("failed to acquire token: %w", err)
	}
	if resp.Token == nil || *resp.Token == "" {
		return fmt.Errorf("no access token available")
	}
	if err := cache.Set(*instance.InstanceId, *resp.Token); err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}
	return nil
}

func (s *SDK) warnf(format string, args ...any) {
	if s.Warnf != nil {
		s.Warnf(format, args...)
	}
}

func (s *SDK) startSandboxInstance(ctx context.Context, sdk *ags.Client, req *ags.StartSandboxInstanceRequest) (*ags.StartSandboxInstanceResponseParams, error) {
	if s.StartSandboxInstance != nil {
		return s.StartSandboxInstance(ctx, sdk, req)
	}
	return callStartSandboxInstance(ctx, sdk, req)
}

func (s *SDK) acquireSandboxInstanceToken(ctx context.Context, sdk *ags.Client, req *ags.AcquireSandboxInstanceTokenRequest) (*ags.AcquireSandboxInstanceTokenResponseParams, error) {
	if s.AcquireSandboxInstanceToken != nil {
		return s.AcquireSandboxInstanceToken(ctx, sdk, req)
	}
	return callAcquireSandboxInstanceToken(ctx, sdk, req)
}

func fillRequest(commandID string, request map[string]any, target jsonRequest) error {
	raw, err := json.Marshal(request)
	if err != nil {
		return err
	}
	if err := requestio.ValidatePayload(commandID, raw); err != nil {
		return err
	}
	if err := target.FromJsonString(string(raw)); err != nil {
		return requestio.ParseError(commandID, err)
	}
	return nil
}

func callStartSandboxInstance(ctx context.Context, sdk *ags.Client, req *ags.StartSandboxInstanceRequest) (*ags.StartSandboxInstanceResponseParams, error) {
	resp, err := sdk.StartSandboxInstanceWithContext(ctx, req)
	if err != nil {
		return nil, client.ClassifyCloudError(err)
	}
	return resp.Response, nil
}

func callDescribeSandboxInstanceList(ctx context.Context, sdk *ags.Client, req *ags.DescribeSandboxInstanceListRequest) (*ags.DescribeSandboxInstanceListResponseParams, error) {
	resp, err := sdk.DescribeSandboxInstanceListWithContext(ctx, req)
	if err != nil {
		return nil, client.ClassifyCloudError(err)
	}
	return resp.Response, nil
}

func callUpdateSandboxInstance(ctx context.Context, sdk *ags.Client, req *ags.UpdateSandboxInstanceRequest) (*ags.UpdateSandboxInstanceResponseParams, error) {
	resp, err := sdk.UpdateSandboxInstanceWithContext(ctx, req)
	if err != nil {
		return nil, client.ClassifyCloudError(err)
	}
	return resp.Response, nil
}

func callPauseSandboxInstance(ctx context.Context, sdk *ags.Client, req *ags.PauseSandboxInstanceRequest) (*ags.PauseSandboxInstanceResponseParams, error) {
	resp, err := sdk.PauseSandboxInstanceWithContext(ctx, req)
	if err != nil {
		return nil, client.ClassifyCloudError(err)
	}
	return resp.Response, nil
}

func callResumeSandboxInstance(ctx context.Context, sdk *ags.Client, req *ags.ResumeSandboxInstanceRequest) (*ags.ResumeSandboxInstanceResponseParams, error) {
	resp, err := sdk.ResumeSandboxInstanceWithContext(ctx, req)
	if err != nil {
		return nil, client.ClassifyCloudError(err)
	}
	return resp.Response, nil
}

func callStopSandboxInstance(ctx context.Context, sdk *ags.Client, req *ags.StopSandboxInstanceRequest) (*ags.StopSandboxInstanceResponseParams, error) {
	resp, err := sdk.StopSandboxInstanceWithContext(ctx, req)
	if err != nil {
		return nil, client.ClassifyCloudError(err)
	}
	return resp.Response, nil
}

func callAcquireSandboxInstanceToken(ctx context.Context, sdk *ags.Client, req *ags.AcquireSandboxInstanceTokenRequest) (*ags.AcquireSandboxInstanceTokenResponseParams, error) {
	resp, err := sdk.AcquireSandboxInstanceTokenWithContext(ctx, req)
	if err != nil {
		return nil, client.ClassifyCloudError(err)
	}
	return resp.Response, nil
}

func callCreateSandboxTool(ctx context.Context, sdk *ags.Client, req *ags.CreateSandboxToolRequest) (*ags.CreateSandboxToolResponseParams, error) {
	resp, err := sdk.CreateSandboxToolWithContext(ctx, req)
	if err != nil {
		return nil, client.ClassifyCloudError(err)
	}
	return resp.Response, nil
}

func callDescribeSandboxToolList(ctx context.Context, sdk *ags.Client, req *ags.DescribeSandboxToolListRequest) (*ags.DescribeSandboxToolListResponseParams, error) {
	resp, err := sdk.DescribeSandboxToolListWithContext(ctx, req)
	if err != nil {
		return nil, client.ClassifyCloudError(err)
	}
	return resp.Response, nil
}

func callUpdateSandboxTool(ctx context.Context, sdk *ags.Client, req *ags.UpdateSandboxToolRequest) (*ags.UpdateSandboxToolResponseParams, error) {
	resp, err := sdk.UpdateSandboxToolWithContext(ctx, req)
	if err != nil {
		return nil, client.ClassifyCloudError(err)
	}
	return resp.Response, nil
}

func callDeleteSandboxTool(ctx context.Context, sdk *ags.Client, req *ags.DeleteSandboxToolRequest) (*ags.DeleteSandboxToolResponseParams, error) {
	resp, err := sdk.DeleteSandboxToolWithContext(ctx, req)
	if err != nil {
		return nil, client.ClassifyCloudError(err)
	}
	return resp.Response, nil
}

func callCreateAPIKey(ctx context.Context, sdk *ags.Client, req *ags.CreateAPIKeyRequest) (*ags.CreateAPIKeyResponseParams, error) {
	resp, err := sdk.CreateAPIKeyWithContext(ctx, req)
	if err != nil {
		return nil, client.ClassifyCloudError(err)
	}
	return resp.Response, nil
}

func callDescribeAPIKeyList(ctx context.Context, sdk *ags.Client, req *ags.DescribeAPIKeyListRequest) (*ags.DescribeAPIKeyListResponseParams, error) {
	resp, err := sdk.DescribeAPIKeyListWithContext(ctx, req)
	if err != nil {
		return nil, client.ClassifyCloudError(err)
	}
	return resp.Response, nil
}

func callDeleteAPIKey(ctx context.Context, sdk *ags.Client, req *ags.DeleteAPIKeyRequest) (*ags.DeleteAPIKeyResponseParams, error) {
	resp, err := sdk.DeleteAPIKeyWithContext(ctx, req)
	if err != nil {
		return nil, client.ClassifyCloudError(err)
	}
	return resp.Response, nil
}

func callCreatePreCacheImageTask(ctx context.Context, sdk *ags.Client, req *ags.CreatePreCacheImageTaskRequest) (*ags.CreatePreCacheImageTaskResponseParams, error) {
	resp, err := sdk.CreatePreCacheImageTaskWithContext(ctx, req)
	if err != nil {
		return nil, client.ClassifyCloudError(err)
	}
	return resp.Response, nil
}

func callDescribePreCacheImageTask(ctx context.Context, sdk *ags.Client, req *ags.DescribePreCacheImageTaskRequest) (*ags.DescribePreCacheImageTaskResponseParams, error) {
	resp, err := sdk.DescribePreCacheImageTaskWithContext(ctx, req)
	if err != nil {
		return nil, client.ClassifyCloudError(err)
	}
	return resp.Response, nil
}

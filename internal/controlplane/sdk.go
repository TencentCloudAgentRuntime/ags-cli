// Package controlplane adapts normalized command requests to TencentCloud AGS
// control-plane API calls.
package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	requestio "github.com/TencentCloudAgentRuntime/ags-cli/internal/cli/request"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/client"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/dataplane/token"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

// SDK routes contract-validated operations through typed or lossless JSON transport.
type SDK struct {
	// Contract overrides embedded metadata for isolated contract tests. CLI wiring leaves it nil.
	Contract                    *apimeta.Spec
	Client                      *ags.Client
	NewClient                   func() (*ags.Client, error)
	StartSandboxInstance        func(context.Context, *ags.Client, *ags.StartSandboxInstanceRequest) (*ags.StartSandboxInstanceResponseParams, error)
	AcquireSandboxInstanceToken func(context.Context, *ags.Client, *ags.AcquireSandboxInstanceTokenRequest) (*ags.AcquireSandboxInstanceTokenResponseParams, error)
	CreateDeployment            func(context.Context, *ags.Client, *ags.CreateDeploymentRequest) (*ags.CreateDeploymentResponseParams, error)
	DeleteDeployment            func(context.Context, *ags.Client, *ags.DeleteDeploymentRequest) (*ags.DeleteDeploymentResponseParams, error)
	DescribeDeployment          func(context.Context, *ags.Client, *ags.DescribeDeploymentRequest) (*ags.DescribeDeploymentResponseParams, error)
	DescribeDeploymentList      func(context.Context, *ags.Client, *ags.DescribeDeploymentListRequest) (*ags.DescribeDeploymentListResponseParams, error)
	ModifyDeployment            func(context.Context, *ags.Client, *ags.ModifyDeploymentRequest) (*ags.ModifyDeploymentResponseParams, error)
	AcquireDeploymentToken      func(context.Context, *ags.Client, *ags.AcquireDeploymentTokenRequest) (*ags.AcquireDeploymentTokenResponseParams, error)
	TokenCache                  *token.Cache
	TokenCacheReady             bool
	Warnf                       func(format string, args ...any)
	// RawSender overrides the signed JSON transport in isolated tests. It is
	// used before typed decoding when the active request or response contract
	// cannot be represented by the AGS SDK, including workflow-only Actions.
	RawSender RawAPISender
}

type jsonRequest interface {
	FromJsonString(string) error
}

// Call executes a generated API action using a map-based request payload.
func (s *SDK) Call(ctx context.Context, action string, request map[string]any) (any, error) {
	spec := s.Contract
	if spec == nil {
		catalog, err := apimeta.Get()
		if err != nil {
			return nil, err
		}
		spec = catalog.Spec
	}
	_, known := spec.Actions[action]
	validationSpec := spec
	if !known {
		validationSpec = apimeta.WorkflowSpec()
	}
	if err := validationSpec.ValidateRequest(action, request); err != nil {
		return nil, output.NewUsageError("INVALID_REQUEST_JSON", invalidContractRequest(action, err).Error(), "Use agr schema for the installed channel's fields.")
	}
	if needsDynamic(spec, action) {
		return s.callDynamic(ctx, action, request)
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	apiClient, err := s.cloudClient()
	if err != nil {
		return nil, err
	}
	switch action {
	case "DeleteSandboxTool":
		req := ags.NewDeleteSandboxToolRequest()
		if err := fillRequest("tool.delete", request, req); err != nil {
			return nil, err
		}
		return callDeleteSandboxTool(ctx, apiClient, req)
	case "StopSandboxInstance":
		req := ags.NewStopSandboxInstanceRequest()
		if err := fillRequest("instance.delete", request, req); err != nil {
			return nil, err
		}
		return callStopSandboxInstance(ctx, apiClient, req)
	case "AcquireSandboxInstanceToken":
		req := ags.NewAcquireSandboxInstanceTokenRequest()
		if err := fillRequest("instance.token", request, req); err != nil {
			return nil, err
		}
		return s.acquireSandboxInstanceToken(ctx, apiClient, req)
	case "CreateDeployment":
		req := ags.NewCreateDeploymentRequest()
		if err := fillRequest("deployment.create", request, req); err != nil {
			return nil, err
		}
		return s.createDeployment(ctx, apiClient, req)
	case "DeleteDeployment":
		req := ags.NewDeleteDeploymentRequest()
		if err := fillRequest("deployment.delete", request, req); err != nil {
			return nil, err
		}
		return s.deleteDeployment(ctx, apiClient, req)
	case "DescribeDeployment":
		req := ags.NewDescribeDeploymentRequest()
		if err := fillRequest("deployment.get", request, req); err != nil {
			return nil, err
		}
		return s.describeDeployment(ctx, apiClient, req)
	case "DescribeDeploymentList":
		req := ags.NewDescribeDeploymentListRequest()
		if err := fillRequest("deployment.list", request, req); err != nil {
			return nil, err
		}
		return s.describeDeploymentList(ctx, apiClient, req)
	case "ModifyDeployment":
		req := ags.NewModifyDeploymentRequest()
		if err := fillRequest("deployment.update", request, req); err != nil {
			return nil, err
		}
		return s.modifyDeployment(ctx, apiClient, req)
	case "AcquireDeploymentToken":
		req := ags.NewAcquireDeploymentTokenRequest()
		if err := fillRequest("deployment.proxy", request, req); err != nil {
			return nil, err
		}
		return s.acquireDeploymentToken(ctx, apiClient, req)
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
		return s.callDynamic(ctx, action, request)
	}
}

// DeleteTool deletes a sandbox tool by ID.
func (s *SDK) DeleteTool(ctx context.Context, toolID string) error {
	_, err := s.Call(ctx, "DeleteSandboxTool", map[string]any{"ToolId": toolID})
	return err
}

func (s *SDK) GetTool(ctx context.Context, toolID string) (apivalue.Object, error) {
	return s.getResource(ctx, "DescribeSandboxToolList", map[string]any{"ToolIds": []string{toolID}}, "SandboxToolSet", "TOOL_NOT_FOUND", toolID)
}

func (s *SDK) DeleteInstance(ctx context.Context, instanceID string) error {
	if _, err := s.Call(ctx, "StopSandboxInstance", map[string]any{"InstanceId": instanceID}); err != nil {
		return err
	}
	if cache := s.cache(); cache != nil {
		_ = cache.Delete(instanceID)
	}
	return nil
}

func (s *SDK) GetInstance(ctx context.Context, instanceID string) (apivalue.Object, error) {
	return s.getResource(ctx, "DescribeSandboxInstanceList", map[string]any{"InstanceIds": []string{instanceID}}, "InstanceSet", "INSTANCE_NOT_FOUND", instanceID)
}

func (s *SDK) GetDeployment(ctx context.Context, deploymentID string) (apivalue.Object, error) {
	return s.getResource(ctx, "DescribeDeployment", map[string]any{"DeploymentId": deploymentID}, "Deployment", "ResourceNotFound.Deployment", deploymentID)
}

// GetDeploymentToken retains the SDK-independent complete response in memory.
func (s *SDK) GetDeploymentToken(ctx context.Context, deploymentID string) (apivalue.Object, error) {
	response, err := s.Call(ctx, "AcquireDeploymentToken", map[string]any{"DeploymentId": deploymentID})
	if err != nil {
		return nil, err
	}
	return apivalue.Decode(response)
}

// IsDeploymentNotFound reports only the exact structured API terminal used by
// asynchronous Deployment deletion.
func (s *SDK) IsDeploymentNotFound(err error) bool {
	var cliErr *output.CLIError
	return errors.As(err, &cliErr) && cliErr.Failure != nil && cliErr.Failure.Code == "ResourceNotFound.Deployment"
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

func (s *SDK) cacheInstanceToken(ctx context.Context, _ *ags.Client, instance *ags.SandboxInstance) error {
	value, err := apivalue.Decode(instance)
	if err != nil {
		return err
	}
	return s.cacheResourceToken(ctx, value)
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

func (s *SDK) createDeployment(ctx context.Context, sdk *ags.Client, req *ags.CreateDeploymentRequest) (*ags.CreateDeploymentResponseParams, error) {
	if s.CreateDeployment != nil {
		return s.CreateDeployment(ctx, sdk, req)
	}
	return callCreateDeployment(ctx, sdk, req)
}

func (s *SDK) deleteDeployment(ctx context.Context, sdk *ags.Client, req *ags.DeleteDeploymentRequest) (*ags.DeleteDeploymentResponseParams, error) {
	if s.DeleteDeployment != nil {
		return s.DeleteDeployment(ctx, sdk, req)
	}
	return callDeleteDeployment(ctx, sdk, req)
}

func (s *SDK) describeDeployment(ctx context.Context, sdk *ags.Client, req *ags.DescribeDeploymentRequest) (*ags.DescribeDeploymentResponseParams, error) {
	if s.DescribeDeployment != nil {
		return s.DescribeDeployment(ctx, sdk, req)
	}
	return callDescribeDeployment(ctx, sdk, req)
}

func (s *SDK) describeDeploymentList(ctx context.Context, sdk *ags.Client, req *ags.DescribeDeploymentListRequest) (*ags.DescribeDeploymentListResponseParams, error) {
	if s.DescribeDeploymentList != nil {
		return s.DescribeDeploymentList(ctx, sdk, req)
	}
	return callDescribeDeploymentList(ctx, sdk, req)
}

func (s *SDK) modifyDeployment(ctx context.Context, sdk *ags.Client, req *ags.ModifyDeploymentRequest) (*ags.ModifyDeploymentResponseParams, error) {
	if s.ModifyDeployment != nil {
		return s.ModifyDeployment(ctx, sdk, req)
	}
	return callModifyDeployment(ctx, sdk, req)
}

func (s *SDK) acquireDeploymentToken(ctx context.Context, sdk *ags.Client, req *ags.AcquireDeploymentTokenRequest) (*ags.AcquireDeploymentTokenResponseParams, error) {
	if s.AcquireDeploymentToken != nil {
		return s.AcquireDeploymentToken(ctx, sdk, req)
	}
	return callAcquireDeploymentToken(ctx, sdk, req)
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

func callCreateDeployment(ctx context.Context, sdk *ags.Client, req *ags.CreateDeploymentRequest) (*ags.CreateDeploymentResponseParams, error) {
	resp, err := sdk.CreateDeploymentWithContext(ctx, req)
	if err != nil {
		return nil, client.ClassifyCloudError(err)
	}
	return resp.Response, nil
}

func callDeleteDeployment(ctx context.Context, sdk *ags.Client, req *ags.DeleteDeploymentRequest) (*ags.DeleteDeploymentResponseParams, error) {
	resp, err := sdk.DeleteDeploymentWithContext(ctx, req)
	if err != nil {
		return nil, client.ClassifyCloudError(err)
	}
	return resp.Response, nil
}

func callDescribeDeployment(ctx context.Context, sdk *ags.Client, req *ags.DescribeDeploymentRequest) (*ags.DescribeDeploymentResponseParams, error) {
	resp, err := sdk.DescribeDeploymentWithContext(ctx, req)
	if err != nil {
		return nil, client.ClassifyCloudError(err)
	}
	return resp.Response, nil
}

func callDescribeDeploymentList(ctx context.Context, sdk *ags.Client, req *ags.DescribeDeploymentListRequest) (*ags.DescribeDeploymentListResponseParams, error) {
	resp, err := sdk.DescribeDeploymentListWithContext(ctx, req)
	if err != nil {
		return nil, client.ClassifyCloudError(err)
	}
	return resp.Response, nil
}

func callModifyDeployment(ctx context.Context, sdk *ags.Client, req *ags.ModifyDeploymentRequest) (*ags.ModifyDeploymentResponseParams, error) {
	resp, err := sdk.ModifyDeploymentWithContext(ctx, req)
	if err != nil {
		return nil, client.ClassifyCloudError(err)
	}
	return resp.Response, nil
}

func callAcquireDeploymentToken(ctx context.Context, sdk *ags.Client, req *ags.AcquireDeploymentTokenRequest) (*ags.AcquireDeploymentTokenResponseParams, error) {
	resp, err := sdk.AcquireDeploymentTokenWithContext(ctx, req)
	if err != nil {
		return nil, client.ClassifyCloudError(err)
	}
	return resp.Response, nil
}

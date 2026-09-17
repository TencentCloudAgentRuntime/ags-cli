package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/client"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	sdkerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
)

func (s *SDK) callDynamic(ctx context.Context, action string, request map[string]any) (any, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	result, err := (RawAPIClient{Sender: s.RawSender}).RawCall(ctx, action, raw)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var sdkErr *sdkerrors.TencentCloudSDKError
		if errors.As(err, &sdkErr) {
			return nil, client.ClassifyCloudError(sdkErr)
		}
		return nil, output.ClassifyError(err)
	}
	envelope, err := apivalue.Decode(result.Response)
	if err != nil {
		return nil, err
	}
	response := envelope.Object("Response")
	if response == nil {
		return nil, fmt.Errorf("%s: response has no Response object", action)
	}
	if failure := response.Object("Error"); failure != nil {
		return nil, client.ClassifyCloudError(sdkerrors.NewTencentCloudSDKError(failure.String("Code"), failure.String("Message"), response.String("RequestId")))
	}
	if action == "StartSandboxInstance" {
		instance := response.Object("Instance")
		if instance == nil || instance.String("InstanceId") == "" {
			return nil, fmt.Errorf("no instance returned from API")
		}
		if err := s.cacheResourceToken(ctx, instance); err != nil {
			s.warnf("Warning: Failed to cache access token: %v\n", err)
		}
	}
	// Preserve the established raw workflow response shape.
	if _, workflow := apimeta.WorkflowSpec().Actions[action]; workflow {
		delete(response, "RequestId")
	}
	return map[string]any(response), nil
}

func (s *SDK) getResource(ctx context.Context, action string, request map[string]any, field, notFound, id string) (apivalue.Object, error) {
	value, err := s.Call(ctx, action, request)
	if err != nil {
		return nil, err
	}
	response, err := apivalue.Decode(value)
	if err != nil {
		return nil, err
	}
	var resource apivalue.Object
	switch field {
	case "Deployment":
		if response[field] != nil {
			resource = response.Object(field)
			if resource == nil {
				return nil, fmt.Errorf("%s: invalid %s object", action, field)
			}
		}
	default:
		if response[field] != nil {
			resources := response.Objects(field)
			if resources == nil {
				return nil, fmt.Errorf("%s: invalid %s array", action, field)
			}
			if len(resources) > 0 {
				resource = resources[0]
				if resource == nil {
					return nil, fmt.Errorf("%s: null resource", action)
				}
			}
		}
	}
	if resource != nil {
		idField := map[string]string{"Deployment": "DeploymentId", "InstanceSet": "InstanceId", "SandboxToolSet": "ToolId"}[field]
		if resource.String(idField) == "" {
			return nil, fmt.Errorf("%s: resource is missing %s", action, idField)
		}
		return resource, nil
	}
	return nil, output.NewNotFoundError(notFound, fmt.Sprintf("resource not found: %s", id), "List resources to find an available ID.")
}

func (s *SDK) cacheResourceToken(ctx context.Context, instance apivalue.Object) error {
	if instance.String("AuthMode") == "NONE" {
		return nil
	}
	id := instance.String("InstanceId")
	if id == "" {
		return fmt.Errorf("instance response is missing InstanceId")
	}
	cache := s.cache()
	if cache == nil {
		return nil
	}
	value, err := s.Call(ctx, "AcquireSandboxInstanceToken", map[string]any{"InstanceId": id})
	if err != nil {
		return err
	}
	response, err := apivalue.Decode(value)
	if err != nil {
		return err
	}
	token := response.String("Token")
	if token == "" {
		return fmt.Errorf("no access token available")
	}
	return cache.Set(id, token)
}

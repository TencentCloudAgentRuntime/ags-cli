package controlplane

import (
	"context"
	"errors"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

func TestSDKCallUsesTypedDeploymentOperations(t *testing.T) {
	config.SetSecretID("sid")
	config.SetSecretKey("skey")
	ctx := context.Background()
	client := &ags.Client{}

	t.Run("create", func(t *testing.T) {
		called := false
		sdk := &SDK{Client: client, CreateDeployment: func(_ context.Context, _ *ags.Client, req *ags.CreateDeploymentRequest) (*ags.CreateDeploymentResponseParams, error) {
			called = true
			if got := derefTestString(req.DeploymentName); got != "workspace-service" {
				t.Fatalf("DeploymentName = %q, want workspace-service", got)
			}
			return &ags.CreateDeploymentResponseParams{Deployment: &ags.Deployment{}}, nil
		}}
		got, err := sdk.Call(ctx, "CreateDeployment", map[string]any{"DeploymentName": "workspace-service"})
		if err != nil || !called {
			t.Fatalf("Call() = (%T, %v), called=%v", got, err, called)
		}
		if _, ok := got.(*ags.CreateDeploymentResponseParams); !ok {
			t.Fatalf("Call() type = %T, want *ags.CreateDeploymentResponseParams", got)
		}
	})

	t.Run("delete", func(t *testing.T) {
		called := false
		sdk := &SDK{Client: client, DeleteDeployment: func(_ context.Context, _ *ags.Client, req *ags.DeleteDeploymentRequest) (*ags.DeleteDeploymentResponseParams, error) {
			called = derefTestString(req.DeploymentId) == "dpl-a1b2c3d4"
			return &ags.DeleteDeploymentResponseParams{}, nil
		}}
		_, err := sdk.Call(ctx, "DeleteDeployment", map[string]any{"DeploymentId": "dpl-a1b2c3d4"})
		if err != nil || !called {
			t.Fatalf("Call() error = %v, called=%v", err, called)
		}
	})

	t.Run("describe", func(t *testing.T) {
		called := false
		sdk := &SDK{Client: client, DescribeDeployment: func(_ context.Context, _ *ags.Client, req *ags.DescribeDeploymentRequest) (*ags.DescribeDeploymentResponseParams, error) {
			called = derefTestString(req.DeploymentId) == "dpl-a1b2c3d4"
			return &ags.DescribeDeploymentResponseParams{Deployment: &ags.Deployment{}}, nil
		}}
		got, err := sdk.Call(ctx, "DescribeDeployment", map[string]any{"DeploymentId": "dpl-a1b2c3d4"})
		if err != nil || !called {
			t.Fatalf("Call() = (%T, %v), called=%v", got, err, called)
		}
		if _, ok := got.(*ags.DescribeDeploymentResponseParams); !ok {
			t.Fatalf("Call() type = %T, want *ags.DescribeDeploymentResponseParams", got)
		}
	})

	t.Run("list", func(t *testing.T) {
		called := false
		sdk := &SDK{Client: client, DescribeDeploymentList: func(_ context.Context, _ *ags.Client, req *ags.DescribeDeploymentListRequest) (*ags.DescribeDeploymentListResponseParams, error) {
			called = req.Limit != nil && *req.Limit == 20
			return &ags.DescribeDeploymentListResponseParams{}, nil
		}}
		_, err := sdk.Call(ctx, "DescribeDeploymentList", map[string]any{"Limit": 20})
		if err != nil || !called {
			t.Fatalf("Call() error = %v, called=%v", err, called)
		}
	})

	t.Run("modify", func(t *testing.T) {
		called := false
		sdk := &SDK{Client: client, ModifyDeployment: func(_ context.Context, _ *ags.Client, req *ags.ModifyDeploymentRequest) (*ags.ModifyDeploymentResponseParams, error) {
			called = derefTestString(req.DeploymentId) == "dpl-a1b2c3d4"
			return &ags.ModifyDeploymentResponseParams{Deployment: &ags.Deployment{}}, nil
		}}
		_, err := sdk.Call(ctx, "ModifyDeployment", map[string]any{"DeploymentId": "dpl-a1b2c3d4"})
		if err != nil || !called {
			t.Fatalf("Call() error = %v, called=%v", err, called)
		}
	})

	t.Run("token", func(t *testing.T) {
		called := false
		sdk := &SDK{Client: client, AcquireDeploymentToken: func(_ context.Context, _ *ags.Client, req *ags.AcquireDeploymentTokenRequest) (*ags.AcquireDeploymentTokenResponseParams, error) {
			called = derefTestString(req.DeploymentId) == "dpl-a1b2c3d4"
			return &ags.AcquireDeploymentTokenResponseParams{}, nil
		}}
		_, err := sdk.Call(ctx, "AcquireDeploymentToken", map[string]any{"DeploymentId": "dpl-a1b2c3d4"})
		if err != nil || !called {
			t.Fatalf("Call() error = %v, called=%v", err, called)
		}
	})
}

func TestSDKDeploymentWorkflowCapabilities(t *testing.T) {
	config.SetSecretID("sid")
	config.SetSecretKey("skey")
	id := "dpl-a1b2c3d4"
	want := &ags.Deployment{DeploymentId: &id}
	sdk := &SDK{
		Client: &ags.Client{},
		DescribeDeployment: func(_ context.Context, _ *ags.Client, req *ags.DescribeDeploymentRequest) (*ags.DescribeDeploymentResponseParams, error) {
			if derefTestString(req.DeploymentId) != id {
				t.Fatalf("DeploymentId = %q", derefTestString(req.DeploymentId))
			}
			return &ags.DescribeDeploymentResponseParams{Deployment: want}, nil
		},
		AcquireDeploymentToken: func(_ context.Context, _ *ags.Client, req *ags.AcquireDeploymentTokenRequest) (*ags.AcquireDeploymentTokenResponseParams, error) {
			if derefTestString(req.DeploymentId) != id {
				t.Fatalf("DeploymentId = %q", derefTestString(req.DeploymentId))
			}
			token, expires := "dpt_secret", "2026-08-26T08:00:00Z"
			return &ags.AcquireDeploymentTokenResponseParams{Token: &token, ExpiresAt: &expires}, nil
		},
	}

	got, err := sdk.GetDeployment(context.Background(), id)
	if err != nil || got != want {
		t.Fatalf("GetDeployment() = (%p, %v), want (%p, nil)", got, err, want)
	}
	credential, err := sdk.GetDeploymentToken(context.Background(), id)
	if err != nil || derefTestString(credential.Token) != "dpt_secret" {
		t.Fatalf("GetDeploymentToken() = (%#v, %v)", credential, err)
	}

	exact := output.NewNotFoundError("ResourceNotFound.Deployment", "missing", "hint")
	other := output.NewNotFoundError("ResourceNotFound.SandboxTool", "missing", "hint")
	if !sdk.IsDeploymentNotFound(exact) || sdk.IsDeploymentNotFound(other) || sdk.IsDeploymentNotFound(errors.New("ResourceNotFound.Deployment")) {
		t.Fatal("IsDeploymentNotFound must match only the structured exact API code")
	}
}

func derefTestString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

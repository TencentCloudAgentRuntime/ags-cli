package get

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/resourcewait"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

type fakeControlPlane struct {
	toolID string
	err    error
	tools  []*ags.SandboxTool
	calls  int
}

func (f *fakeControlPlane) GetTool(_ context.Context, toolID string) (*ags.SandboxTool, error) {
	f.toolID = toolID
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if len(f.tools) > 0 {
		index := f.calls - 1
		if index >= len(f.tools) {
			index = len(f.tools) - 1
		}
		return f.tools[index], nil
	}
	id := toolID
	return &ags.SandboxTool{ToolId: &id}, nil
}

func TestModuleDescriptorIncludesWait(t *testing.T) {
	for _, flag := range Module().Descriptor.Spec.Flags {
		if flag.Name == "wait" && flag.Type == command.FlagBool && flag.Workflow {
			return
		}
	}
	t.Fatal("tool get descriptor does not include workflow --wait")
}

func TestModuleWaitsForToolTerminalState(t *testing.T) {
	creating := "CREATING"
	active := "ACTIVE"
	cp := &fakeControlPlane{tools: []*ags.SandboxTool{
		{Status: &creating},
		{Status: &active},
	}}
	runtime, err := Module().Build(command.Deps{
		ControlPlane: cp,
		Values: map[string]any{resourcewait.OptionsKey: resourcewait.Options{
			Interval: time.Millisecond,
			Timeout:  50 * time.Millisecond,
		}},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Args: []string{"sdt-unit"},
		Flags: map[string]command.FlagValue{
			"wait": {Name: "wait", Type: command.FlagBool, Bool: true},
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if cp.calls != 2 {
		t.Fatalf("GetTool calls = %d, want 2", cp.calls)
	}
	if result.Data.(map[string]any)["Status"] != active {
		t.Fatalf("data = %#v", result.Data)
	}
}

func TestModuleExecutesToolGetter(t *testing.T) {
	cp := &fakeControlPlane{}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"sdt-unit"},
		ArgValues: map[string]string{"tool-id": "sdt-unit"},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if cp.toolID != "sdt-unit" {
		t.Fatalf("toolID = %q, want sdt-unit", cp.toolID)
	}
	data, ok := result.Data.(map[string]any)
	if !ok || data["ToolId"] != "sdt-unit" {
		t.Fatalf("result.Data = %#v", result.Data)
	}
	var text bytes.Buffer
	result.Text(&text)
	if !strings.Contains(text.String(), "ID:") || !strings.Contains(text.String(), "sdt-unit") {
		t.Fatalf("text = %q", text.String())
	}
}

func TestModuleRequiresControlPlane(t *testing.T) {
	_, err := Module().Build(command.Deps{})
	if err == nil {
		t.Fatalf("expected missing control plane error")
	}
}

func TestModuleFallsBackToPositionalArgs(t *testing.T) {
	cp := &fakeControlPlane{}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{Args: []string{"sdt-arg"}})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if cp.toolID != "sdt-arg" {
		t.Fatalf("toolID = %q, want sdt-arg", cp.toolID)
	}
}

func TestModuleReturnsControlPlaneError(t *testing.T) {
	cp := &fakeControlPlane{err: errors.New("boom")}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{
		ArgValues: map[string]string{"tool-id": "sdt-unit"},
	})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("error = %v, want boom", err)
	}
}

func TestModuleRejectsMissingToolID(t *testing.T) {
	cp := &fakeControlPlane{}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{})
	if err == nil || !strings.Contains(err.Error(), "missing tool id") {
		t.Fatalf("error = %v, want missing tool id", err)
	}
}

func TestRenderToolDetailsIncludesOptionalFields(t *testing.T) {
	id := "sdt-unit"
	name := "tool"
	toolType := "code-interpreter"
	desc := "description"
	created := "2026-05-21T10:00:00Z"
	network := "SANDBOX"
	role := "qcs::role"
	tagKeyA := "alpha"
	tagValueA := "1"
	tagKeyZ := "zeta"
	tagValueZ := "2"
	mountName := "workspace"
	mountPath := "/workspace"
	bucket := "bucket"
	bucketPath := "/src"
	readOnly := true
	tool := &ags.SandboxTool{
		ToolId:      &id,
		ToolName:    &name,
		ToolType:    &toolType,
		Description: &desc,
		CreateTime:  &created,
		RoleArn:     &role,
		Tags:        []*ags.Tag{{Key: &tagKeyZ, Value: &tagValueZ}, {Key: &tagKeyA, Value: &tagValueA}},
		NetworkConfiguration: &ags.NetworkConfiguration{
			NetworkMode: &network,
		},
		StorageMounts: []*ags.StorageMount{{
			Name:      &mountName,
			MountPath: &mountPath,
			ReadOnly:  &readOnly,
			StorageSource: &ags.StorageSource{Cos: &ags.CosStorageSource{
				BucketName: &bucket,
				BucketPath: &bucketPath,
			}},
		}},
	}
	var text bytes.Buffer
	renderToolDetails(&text, tool)
	got := text.String()
	for _, want := range []string{"NetworkMode:", "SANDBOX", "alpha=1, zeta=2", "RoleArn:", "StorageMounts:", "Bucket: bucket", "RO:     true", "05-21 10:00"} {
		if !strings.Contains(got, want) {
			t.Fatalf("text missing %q: %s", want, got)
		}
	}
	data := canonicalToolData(tool)
	if data["ToolId"] != id || data["Tags"].(map[string]string)["alpha"] != "1" {
		t.Fatalf("data = %#v", data)
	}
}

package fork

import (
	"context"
	"errors"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

type fakeControlPlane struct {
	sourceTool *ags.SandboxTool
	getErr     error
	callErr    error
	action     string
	request    map[string]any
}

func (f *fakeControlPlane) GetTool(_ context.Context, toolID string) (*ags.SandboxTool, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.sourceTool != nil {
		return f.sourceTool, nil
	}
	return sourceTool(toolID), nil
}

func (f *fakeControlPlane) Call(_ context.Context, action string, request map[string]any) (any, error) {
	f.action = action
	f.request = request
	if f.callErr != nil {
		return nil, f.callErr
	}
	return map[string]any{"ToolId": "sdt-new"}, nil
}

func TestModuleCopiesCreateCapableFields(t *testing.T) {
	cp := &fakeControlPlane{}
	runFork(t, cp, command.Request{
		Args: []string{"sdt-source"},
		Flags: map[string]command.FlagValue{
			"tool-name": {Name: "tool-name", Type: command.FlagString, String: "copy", Changed: true},
		},
	})
	if cp.action != "CreateSandboxTool" {
		t.Fatalf("action = %q", cp.action)
	}
	if cp.request["ToolName"] != "copy" {
		t.Fatalf("ToolName = %#v", cp.request["ToolName"])
	}
	for _, key := range []string{"ToolType", "NetworkConfiguration", "Description", "DefaultTimeout", "Tags", "RoleArn", "StorageMounts", "CustomConfiguration", "LogConfiguration", "Persistent"} {
		if _, ok := cp.request[key]; !ok {
			t.Fatalf("request missing copied field %s: %#v", key, cp.request)
		}
	}
	for _, key := range []string{"ToolId", "Status", "StatusReason", "CreateTime", "UpdateTime", "ClientToken"} {
		if _, ok := cp.request[key]; ok {
			t.Fatalf("request copied excluded field %s: %#v", key, cp.request)
		}
	}
	if cp.request["DefaultTimeout"] != "300s" {
		t.Fatalf("DefaultTimeout = %#v", cp.request["DefaultTimeout"])
	}
	custom := cp.request["CustomConfiguration"].(*ags.CustomConfiguration)
	if custom.ImageRegistryType == nil || *custom.ImageRegistryType != "enterprise" {
		t.Fatalf("ImageRegistryType = %#v, want enterprise", custom.ImageRegistryType)
	}
}

func TestModuleAppliesExplicitOverrides(t *testing.T) {
	cp := &fakeControlPlane{}
	runFork(t, cp, command.Request{
		Args: []string{"sdt-source"},
		Flags: map[string]command.FlagValue{
			"tool-name":             {Name: "tool-name", Type: command.FlagString, String: "copy", Changed: true},
			"tool-type":             {Name: "tool-type", Type: command.FlagString, String: "custom", Changed: true},
			"description":           {Name: "description", Type: command.FlagString, String: "", Changed: true},
			"default-timeout":       {Name: "default-timeout", Type: command.FlagString, String: "1h", Changed: true},
			"network-configuration": {Name: "network-configuration", Type: command.FlagString, String: `{"NetworkMode":"PUBLIC"}`, Changed: true},
			"tags":                  {Name: "tags", Type: command.FlagString, String: `[{"Key":"team","Value":"qa"}]`, Changed: true},
			"role-arn":              {Name: "role-arn", Type: command.FlagString, String: "", Changed: true},
			"client-token":          {Name: "client-token", Type: command.FlagString, String: "tok", Changed: true},
			"persistent":            {Name: "persistent", Type: command.FlagBool, Bool: false, Changed: true},
		},
	})
	if cp.request["ToolType"] != "custom" {
		t.Fatalf("ToolType = %#v", cp.request["ToolType"])
	}
	if cp.request["Description"] != "" {
		t.Fatalf("Description = %#v, want explicit empty string", cp.request["Description"])
	}
	if cp.request["RoleArn"] != "" {
		t.Fatalf("RoleArn = %#v, want explicit empty string", cp.request["RoleArn"])
	}
	if cp.request["DefaultTimeout"] != "1h" || cp.request["ClientToken"] != "tok" {
		t.Fatalf("request = %#v", cp.request)
	}
	if cp.request["Persistent"] != false {
		t.Fatalf("Persistent = %#v, want false", cp.request["Persistent"])
	}
	network := cp.request["NetworkConfiguration"].(map[string]any)
	if network["NetworkMode"] != "PUBLIC" {
		t.Fatalf("NetworkConfiguration = %#v", network)
	}
}

func TestModuleKeepsSourcePersistentWhenFlagOmitted(t *testing.T) {
	cp := &fakeControlPlane{sourceTool: sourceTool("sdt-source")}
	*cp.sourceTool.Persistent = true
	runFork(t, cp, command.Request{
		Args: []string{"sdt-source"},
		Flags: map[string]command.FlagValue{
			"tool-name": {Name: "tool-name", Type: command.FlagString, String: "copy", Changed: true},
		},
	})
	if cp.request["Persistent"] != true {
		t.Fatalf("Persistent = %#v, want source true", cp.request["Persistent"])
	}
}

func TestModuleDoesNotCopyClientToken(t *testing.T) {
	cp := &fakeControlPlane{}
	runFork(t, cp, command.Request{
		Args: []string{"sdt-source"},
		Flags: map[string]command.FlagValue{
			"tool-name": {Name: "tool-name", Type: command.FlagString, String: "copy", Changed: true},
		},
	})
	if _, ok := cp.request["ClientToken"]; ok {
		t.Fatalf("ClientToken copied from source: %#v", cp.request)
	}
}

func TestModuleFiltersInheritedQcsTags(t *testing.T) {
	source := sourceTool("sdt-source")
	source.Tags = []*ags.Tag{
		{Key: strPtr("env"), Value: strPtr("unit")},
		{Key: strPtr("qcs:project-123"), Value: strPtr("internal")},
		{Key: strPtr("qcs-region-gz"), Value: strPtr("internal")},
	}
	cp := &fakeControlPlane{sourceTool: source}
	runFork(t, cp, command.Request{
		Args: []string{"sdt-source"},
		Flags: map[string]command.FlagValue{
			"tool-name": {Name: "tool-name", Type: command.FlagString, String: "copy", Changed: true},
		},
	})
	tags := cp.request["Tags"].([]*ags.Tag)
	if len(tags) != 1 || *tags[0].Key != "env" {
		t.Fatalf("Tags = %#v, want only env tag", tags)
	}
}

func TestModuleOmitsTagsWhenOnlyInheritedQcsTagsRemain(t *testing.T) {
	source := sourceTool("sdt-source")
	source.Tags = []*ags.Tag{{Key: strPtr("qcs:project-123"), Value: strPtr("internal")}}
	cp := &fakeControlPlane{sourceTool: source}
	runFork(t, cp, command.Request{
		Args: []string{"sdt-source"},
		Flags: map[string]command.FlagValue{
			"tool-name": {Name: "tool-name", Type: command.FlagString, String: "copy", Changed: true},
		},
	})
	if _, ok := cp.request["Tags"]; ok {
		t.Fatalf("Tags should be omitted when only qcs tags remain: %#v", cp.request["Tags"])
	}
}

func TestModuleRejectsMissingSourceID(t *testing.T) {
	cp := &fakeControlPlane{}
	err := runForkErr(t, cp, command.Request{
		Flags: map[string]command.FlagValue{
			"tool-name": {Name: "tool-name", Type: command.FlagString, String: "copy", Changed: true},
		},
	})
	if cliErr, ok := err.(*output.CLIError); !ok || cliErr.Failure.Code != "MISSING_REQUIRED_ARG" {
		t.Fatalf("error = %#v, want MISSING_REQUIRED_ARG", err)
	}
}

func TestModuleRejectsMissingToolName(t *testing.T) {
	cp := &fakeControlPlane{}
	err := runForkErr(t, cp, command.Request{Args: []string{"sdt-source"}})
	if cliErr, ok := err.(*output.CLIError); !ok || cliErr.Failure.Code != "MISSING_REQUIRED_FLAG" {
		t.Fatalf("error = %#v, want MISSING_REQUIRED_FLAG", err)
	}
}

func TestModulePropagatesLookupFailure(t *testing.T) {
	want := errors.New("lookup failed")
	cp := &fakeControlPlane{getErr: want}
	err := runForkErr(t, cp, command.Request{
		Args: []string{"sdt-source"},
		Flags: map[string]command.FlagValue{
			"tool-name": {Name: "tool-name", Type: command.FlagString, String: "copy", Changed: true},
		},
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestModulePropagatesCreateFailure(t *testing.T) {
	want := errors.New("create failed")
	cp := &fakeControlPlane{callErr: want}
	err := runForkErr(t, cp, command.Request{
		Args: []string{"sdt-source"},
		Flags: map[string]command.FlagValue{
			"tool-name": {Name: "tool-name", Type: command.FlagString, String: "copy", Changed: true},
		},
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func runFork(t *testing.T, cp *fakeControlPlane, req command.Request) *command.Result {
	t.Helper()
	result, err := runForkResult(t, cp, req)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	return result
}

func runForkErr(t *testing.T, cp *fakeControlPlane, req command.Request) error {
	t.Helper()
	_, err := runForkResult(t, cp, req)
	if err == nil {
		t.Fatal("Run returned nil error")
	}
	return err
}

func runForkResult(t *testing.T, cp *fakeControlPlane, req command.Request) (*command.Result, error) {
	t.Helper()
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	return runtime.Handler.Run(context.Background(), req)
}

func sourceTool(id string) *ags.SandboxTool {
	toolType := "code-interpreter"
	description := "source description"
	networkMode := "SANDBOX"
	timeout := uint64(300)
	tagKey := "env"
	tagValue := "unit"
	roleArn := "qcs::cam::uin/100000:roleName/source-role"
	mountName := "data"
	mountPath := "/data"
	readOnly := true
	image := "repo/app:latest"
	registryType := "TCR"
	imageDigest := "sha256:read-only"
	commandValue := "serve"
	logFile := "/logs/app.log"
	persistent := true
	status := "ACTIVE"
	statusReason := "ready"
	createTime := "2026-01-01T00:00:00Z"
	updateTime := "2026-01-02T00:00:00Z"
	return &ags.SandboxTool{
		ToolId:                &id,
		ToolName:              strPtr("source"),
		ToolType:              &toolType,
		Status:                &status,
		Description:           &description,
		Persistent:            &persistent,
		DefaultTimeoutSeconds: &timeout,
		NetworkConfiguration:  &ags.NetworkConfiguration{NetworkMode: &networkMode},
		Tags:                  []*ags.Tag{{Key: &tagKey, Value: &tagValue}},
		CreateTime:            &createTime,
		UpdateTime:            &updateTime,
		RoleArn:               &roleArn,
		StorageMounts:         []*ags.StorageMount{{Name: &mountName, MountPath: &mountPath, ReadOnly: &readOnly}},
		CustomConfiguration:   &ags.CustomConfigurationDetail{Image: &image, ImageRegistryType: &registryType, ImageDigest: &imageDigest, Command: []*string{&commandValue}},
		LogConfiguration:      &ags.LogConfiguration{LogSources: &ags.LogSources{Files: []*string{&logFile}}},
		StatusReason:          &statusReason,
	}
}

func strPtr(value string) *string {
	return &value
}

var _ ControlPlane = (*fakeControlPlane)(nil)
var _ apicli.ControlPlane = (*fakeControlPlane)(nil)

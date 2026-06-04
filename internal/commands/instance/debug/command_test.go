package debug

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/iostreams"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

type fakeControlPlane struct {
	sourceTool        *ags.SandboxTool
	debugTool         *ags.SandboxTool
	instance          *ags.SandboxInstance
	getIDs            []string
	actions           []string
	requests          []map[string]any
	deletes           []string
	createResp        any
	startResp         any
	err               error
	toolReadyErr      error
	instanceReadyErr  error
	deleteToolErr     error
	deleteInstanceErr error
}

func (f *fakeControlPlane) GetTool(_ context.Context, toolID string) (*ags.SandboxTool, error) {
	f.getIDs = append(f.getIDs, "tool:"+toolID)
	if f.err != nil {
		return nil, f.err
	}
	if toolID == "sdt-source" {
		return f.sourceTool, nil
	}
	if f.toolReadyErr != nil {
		return nil, f.toolReadyErr
	}
	if f.debugTool != nil {
		return f.debugTool, nil
	}
	active := "ACTIVE"
	return &ags.SandboxTool{ToolId: strPtr(toolID), ToolName: strPtr("source-debug"), Status: &active}, nil
}

func (f *fakeControlPlane) Call(_ context.Context, action string, request map[string]any) (any, error) {
	f.actions = append(f.actions, action)
	f.requests = append(f.requests, request)
	if f.err != nil {
		return nil, f.err
	}
	switch action {
	case "CreateSandboxTool":
		if f.createResp != nil {
			return f.createResp, nil
		}
		id := "sdt-debug"
		return &ags.CreateSandboxToolResponseParams{ToolId: &id}, nil
	case "StartSandboxInstance":
		if f.startResp != nil {
			return f.startResp, nil
		}
		id := "ins-debug"
		status := "STARTING"
		return &ags.StartSandboxInstanceResponseParams{Instance: &ags.SandboxInstance{InstanceId: &id, Status: &status}}, nil
	default:
		return nil, errors.New("unexpected action: " + action)
	}
}

func (f *fakeControlPlane) GetInstance(_ context.Context, instanceID string) (*ags.SandboxInstance, error) {
	f.getIDs = append(f.getIDs, "instance:"+instanceID)
	if f.instanceReadyErr != nil {
		return nil, f.instanceReadyErr
	}
	if f.instance != nil {
		return f.instance, nil
	}
	running := "RUNNING"
	return &ags.SandboxInstance{InstanceId: strPtr(instanceID), ToolId: strPtr("sdt-debug"), ToolName: strPtr("source-debug"), Status: &running}, nil
}

func (f *fakeControlPlane) DeleteTool(_ context.Context, toolID string) error {
	f.deletes = append(f.deletes, "tool:"+toolID)
	return f.deleteToolErr
}

func (f *fakeControlPlane) DeleteInstance(_ context.Context, instanceID string) error {
	f.deletes = append(f.deletes, "instance:"+instanceID)
	return f.deleteInstanceErr
}

func TestModuleRunsFullDebugWorkflow(t *testing.T) {
	cp := &fakeControlPlane{sourceTool: sourceTool()}
	runtime, err := Module().Build(command.Deps{
		ControlPlane: cp,
		Now:          func() time.Time { return time.Date(2026, 6, 1, 10, 11, 12, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Flags: map[string]command.FlagValue{
			"tool-id":      {Name: "tool-id", Type: command.FlagString, String: "sdt-source", Changed: true},
			"client-token": {Name: "client-token", Type: command.FlagString, String: "token-1", Changed: true},
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, want := strings.Join(cp.getIDs, ","), "tool:sdt-source,tool:sdt-debug,instance:ins-debug"; got != want {
		t.Fatalf("lookups = %q, want %q", got, want)
	}
	if got, want := strings.Join(cp.actions, ","), "CreateSandboxTool,StartSandboxInstance"; got != want {
		t.Fatalf("actions = %q, want %q", got, want)
	}
	createReq := cp.requests[0]
	if createReq["ToolName"] != "source-debug-20260601101112" {
		t.Fatalf("ToolName = %#v", createReq["ToolName"])
	}
	if createReq["ToolType"] != "custom" || createReq["DefaultTimeout"] != "300s" || createReq["ClientToken"] != "token-1" {
		t.Fatalf("request = %#v", createReq)
	}
	custom := createReq["CustomConfiguration"].(map[string]any)
	if _, ok := custom["ImageDigest"]; ok {
		t.Fatalf("CustomConfiguration leaked ImageDigest: %#v", custom)
	}
	if got := stringSlice(custom["Command"]); len(got) != 1 || got[0] != "/envd" {
		t.Fatalf("Command = %#v", custom["Command"])
	}
	if got := stringSlice(custom["Args"]); len(got) != 0 {
		t.Fatalf("Args = %#v, want empty", custom["Args"])
	}
	ports := custom["Ports"].([]map[string]any)
	if len(ports) != 1 || ports[0]["Name"] != debugPortName || ports[0]["Port"] != debugPort || ports[0]["Protocol"] != debugPortProtocol {
		t.Fatalf("Ports = %#v", ports)
	}
	probe := custom["Probe"].(map[string]any)
	httpGet := probe["HttpGet"].(map[string]any)
	if httpGet["Scheme"] != debugProbeScheme || httpGet["Port"] != debugPort || httpGet["Path"] != debugHealthPath {
		t.Fatalf("Probe.HttpGet = %#v", httpGet)
	}
	if probe["ReadyTimeoutMs"] != 30000 ||
		probe["ProbeTimeoutMs"] != 2000 ||
		probe["ProbePeriodMs"] != 1000 ||
		probe["SuccessThreshold"] != 1 ||
		probe["FailureThreshold"] != 30 {
		t.Fatalf("Probe = %#v", probe)
	}

	mounts := createReq["StorageMounts"].([]map[string]any)
	if len(mounts) != 2 {
		t.Fatalf("StorageMounts len = %d, want 2: %#v", len(mounts), mounts)
	}
	if mounts[0]["Name"] != "workspace" || mounts[0]["MountPath"] != "/workspace" {
		t.Fatalf("copied mount = %#v", mounts[0])
	}
	added := mounts[1]
	if added["Name"] != envdMountName || added["MountPath"] != envdMountPath || added["ReadOnly"] != true {
		t.Fatalf("added mount = %#v", added)
	}
	image := added["StorageSource"].(map[string]any)["Image"].(map[string]any)
	if image["Reference"] != envdImageRef || image["ImageRegistryType"] != envdRegistryType || image["SubPath"] != envdImageSubPath {
		t.Fatalf("image mount = %#v", image)
	}
	startReq := cp.requests[1]
	if startReq["ToolId"] != "sdt-debug" || startReq["Timeout"] != defaultTimeout {
		t.Fatalf("start request = %#v", startReq)
	}

	data := result.Data.(map[string]any)
	if data["ToolId"] != "sdt-debug" || data["SourceToolId"] != "sdt-source" || data["InstanceId"] != "ins-debug" || data["Status"] != "RUNNING" {
		t.Fatalf("data = %#v", data)
	}
	if data["Connection"].(map[string]string)["Login"] != "agr instance login ins-debug --user \"YOUR_USER\"" {
		t.Fatalf("connection = %#v", data["Connection"])
	}
	if len(result.Effects) != 2 || result.Effects[0].Resource != "tool" || result.Effects[1].Resource != "instance" {
		t.Fatalf("effects = %#v", result.Effects)
	}
	var text bytes.Buffer
	result.Text(&text)
	if !strings.Contains(text.String(), "Debug instance ready: ins-debug") || !strings.Contains(text.String(), "agr instance login ins-debug --user") || !strings.Contains(text.String(), envdImageRef) {
		t.Fatalf("text = %q", text.String())
	}
}

func TestModulePassesInstanceTimeout(t *testing.T) {
	cp := &fakeControlPlane{sourceTool: sourceTool()}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{
		Flags: map[string]command.FlagValue{
			"tool-id": {Name: "tool-id", Type: command.FlagString, String: "sdt-source", Changed: true},
			"timeout": {Name: "timeout", Type: command.FlagString, String: "30m", Changed: true},
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got := cp.requests[1]["Timeout"]; got != "30m" {
		t.Fatalf("Timeout = %#v, want 30m", got)
	}
}

func TestModuleCleansUpOnToolReadyFailure(t *testing.T) {
	failed := "FAILED"
	cp := &fakeControlPlane{
		sourceTool: sourceTool(),
		debugTool:  &ags.SandboxTool{ToolId: strPtr("sdt-debug"), Status: &failed},
	}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{Flags: map[string]command.FlagValue{"tool-id": {Name: "tool-id", Type: command.FlagString, String: "sdt-source", Changed: true}}})
	if err == nil || !strings.Contains(err.Error(), "debug tool sdt-debug failed") {
		t.Fatalf("error = %v, want tool ready failure", err)
	}
	if got, want := strings.Join(cp.deletes, ","), "tool:sdt-debug"; got != want {
		t.Fatalf("deletes = %q, want %q", got, want)
	}
}

func TestModuleCleansUpInstanceThenToolOnInstanceFailure(t *testing.T) {
	failed := "FAILED"
	cp := &fakeControlPlane{
		sourceTool: sourceTool(),
		instance:   &ags.SandboxInstance{InstanceId: strPtr("ins-debug"), Status: &failed},
	}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{Flags: map[string]command.FlagValue{"tool-id": {Name: "tool-id", Type: command.FlagString, String: "sdt-source", Changed: true}}})
	if err == nil || !strings.Contains(err.Error(), "debug instance ins-debug failed") {
		t.Fatalf("error = %v, want instance ready failure", err)
	}
	if got, want := strings.Join(cp.deletes, ","), "instance:ins-debug,tool:sdt-debug"; got != want {
		t.Fatalf("deletes = %q, want %q", got, want)
	}
}

func TestModuleReturnsCleanupWarnings(t *testing.T) {
	ios, _, _, stderr := iostreams.Test()
	failed := "FAILED"
	cp := &fakeControlPlane{
		sourceTool:        sourceTool(),
		instance:          &ags.SandboxInstance{InstanceId: strPtr("ins-debug"), Status: &failed},
		deleteInstanceErr: errors.New("delete instance boom"),
		deleteToolErr:     errors.New("delete tool boom"),
	}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp, IO: ios})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{Flags: map[string]command.FlagValue{"tool-id": {Name: "tool-id", Type: command.FlagString, String: "sdt-source", Changed: true}}})
	if err == nil || !strings.Contains(err.Error(), "debug instance ins-debug failed") {
		t.Fatalf("error = %v, want primary instance failure", err)
	}
	warnings := stderr.String()
	if !strings.Contains(warnings, "Warning: failed to cleanup debug instance ins-debug: delete instance boom") ||
		!strings.Contains(warnings, "Warning: failed to cleanup debug tool sdt-debug: delete tool boom") {
		t.Fatalf("stderr = %q", warnings)
	}
}

func TestModuleFiltersInheritedQcsTags(t *testing.T) {
	tool := sourceTool()
	tool.Tags = []*ags.Tag{
		{Key: strPtr("env"), Value: strPtr("unit")},
		{Key: strPtr("qcs:project-123"), Value: strPtr("internal")},
		{Key: strPtr("qcs-region-gz"), Value: strPtr("internal")},
	}
	cp := &fakeControlPlane{sourceTool: tool}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{Flags: map[string]command.FlagValue{"tool-id": {Name: "tool-id", Type: command.FlagString, String: "sdt-source", Changed: true}}})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	tags := cp.requests[0]["Tags"].([]*ags.Tag)
	if len(tags) != 1 || *tags[0].Key != "env" {
		t.Fatalf("Tags = %#v, want only env tag", tags)
	}
}

func TestModuleOmitsTagsWhenOnlyInheritedQcsTagsRemain(t *testing.T) {
	tool := sourceTool()
	tool.Tags = []*ags.Tag{{Key: strPtr("qcs:project-123"), Value: strPtr("internal")}}
	cp := &fakeControlPlane{sourceTool: tool}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{Flags: map[string]command.FlagValue{"tool-id": {Name: "tool-id", Type: command.FlagString, String: "sdt-source", Changed: true}}})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if _, ok := cp.requests[0]["Tags"]; ok {
		t.Fatalf("Tags should be omitted when only qcs tags remain: %#v", cp.requests[0]["Tags"])
	}
}

func TestModuleRejectsMissingToolID(t *testing.T) {
	cp := &fakeControlPlane{sourceTool: sourceTool()}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{})
	if cliErr, ok := err.(*output.CLIError); !ok || cliErr.Failure.Code != "MISSING_REQUIRED_FLAG" {
		t.Fatalf("error = %#v, want MISSING_REQUIRED_FLAG", err)
	}
}

func TestModuleRejectsDebugMountConflict(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mount *ags.StorageMount
	}{
		{name: "name", mount: &ags.StorageMount{Name: strPtr(envdMountName), MountPath: strPtr("/other")}},
		{name: "path", mount: &ags.StorageMount{Name: strPtr("other"), MountPath: strPtr(envdMountPath)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := sourceTool()
			tool.StorageMounts = []*ags.StorageMount{tc.mount}
			cp := &fakeControlPlane{sourceTool: tool}
			runtime, err := Module().Build(command.Deps{ControlPlane: cp})
			if err != nil {
				t.Fatalf("Build returned error: %v", err)
			}
			_, err = runtime.Handler.Run(context.Background(), command.Request{Flags: map[string]command.FlagValue{"tool-id": {Name: "tool-id", Type: command.FlagString, String: "sdt-source", Changed: true}}})
			if cliErr, ok := err.(*output.CLIError); !ok || cliErr.Failure.Code != "DEBUG_MOUNT_CONFLICT" {
				t.Fatalf("error = %#v, want DEBUG_MOUNT_CONFLICT", err)
			}
			if len(cp.actions) != 0 {
				t.Fatalf("Call executed despite conflict")
			}
		})
	}
}

func TestModuleRejectsSourceWithoutRoleArn(t *testing.T) {
	tool := sourceTool()
	tool.RoleArn = nil
	cp := &fakeControlPlane{sourceTool: tool}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{Flags: map[string]command.FlagValue{"tool-id": {Name: "tool-id", Type: command.FlagString, String: "sdt-source", Changed: true}}})
	if cliErr, ok := err.(*output.CLIError); !ok || cliErr.Failure.Code != "DEBUG_ROLE_ARN_REQUIRED" {
		t.Fatalf("error = %#v, want DEBUG_ROLE_ARN_REQUIRED", err)
	}
	if len(cp.actions) != 0 {
		t.Fatalf("Call executed despite missing RoleArn")
	}
}

func TestModuleReturnsControlPlaneErrors(t *testing.T) {
	cp := &fakeControlPlane{sourceTool: sourceTool(), err: errors.New("boom")}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{Flags: map[string]command.FlagValue{"tool-id": {Name: "tool-id", Type: command.FlagString, String: "sdt-source", Changed: true}}})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("error = %v, want boom", err)
	}
}

func TestDefaultDebugToolNameTruncatesToAPILimit(t *testing.T) {
	got := defaultDebugToolName(strings.Repeat("a", 80), "sdt-source", time.Date(2026, 6, 1, 10, 11, 12, 0, time.UTC))
	if len(got) > defaultNameMaxLen {
		t.Fatalf("name len = %d, want <= %d: %s", len(got), defaultNameMaxLen, got)
	}
	if !strings.HasSuffix(got, "-debug-20260601101112") {
		t.Fatalf("name = %q", got)
	}
}

func sourceTool() *ags.SandboxTool {
	timeout := uint64(300)
	network := "PUBLIC"
	persistent := true
	role := "qcs::role"
	return &ags.SandboxTool{
		ToolId:                strPtr("sdt-source"),
		ToolName:              strPtr("source"),
		ToolType:              strPtr("custom"),
		Description:           strPtr("source description"),
		Persistent:            &persistent,
		DefaultTimeoutSeconds: &timeout,
		RoleArn:               &role,
		NetworkConfiguration:  &ags.NetworkConfiguration{NetworkMode: &network},
		Tags:                  []*ags.Tag{{Key: strPtr("env"), Value: strPtr("unit")}},
		StorageMounts: []*ags.StorageMount{{
			Name:      strPtr("workspace"),
			MountPath: strPtr("/workspace"),
			ReadOnly:  boolPtr(true),
			StorageSource: &ags.StorageSource{Image: &ags.ImageStorageSource{
				Reference:         strPtr("example.com/workspace:latest"),
				ImageRegistryType: strPtr("personal"),
				SubPath:           strPtr("/data"),
				Digest:            strPtr("sha256:abc"),
			}},
		}},
		CustomConfiguration: &ags.CustomConfigurationDetail{
			Image:             strPtr("example.com/source:latest"),
			ImageRegistryType: strPtr("personal"),
			ImageDigest:       strPtr("sha256:def"),
			Command:           []*string{strPtr("/bin/app")},
			Args:              []*string{strPtr("--serve")},
			Ports: []*ags.PortConfiguration{{
				Name:     strPtr("app"),
				Port:     int64Ptr(8080),
				Protocol: strPtr("TCP"),
			}},
			Probe: &ags.ProbeConfiguration{
				HttpGet: &ags.HttpGetAction{
					Path:   strPtr("/ready"),
					Port:   int64Ptr(8080),
					Scheme: strPtr("HTTP"),
				},
				ReadyTimeoutMs:   int64Ptr(10000),
				ProbeTimeoutMs:   int64Ptr(5000),
				ProbePeriodMs:    int64Ptr(10000),
				SuccessThreshold: int64Ptr(1),
				FailureThreshold: int64Ptr(3),
			},
		},
		LogConfiguration: &ags.LogConfiguration{},
	}
}

func stringSlice(value any) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, item.(string))
		}
		return out
	default:
		return nil
	}
}

func strPtr(value string) *string { return &value }

func boolPtr(value bool) *bool { return &value }

func int64Ptr(value int64) *int64 { return &value }

var _ ControlPlane = (*fakeControlPlane)(nil)

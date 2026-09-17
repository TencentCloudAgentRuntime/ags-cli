package fork

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/resourcewait"
	toolcreate "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/tool/create"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

type fakeControlPlane struct {
	sourceTool *ags.SandboxTool
	newStatus  string
	getErr     error
	callErr    error
	action     string
	request    map[string]any
	getIDs     []string
	callCount  int
}

var forkReplacementFields = map[string]string{
	"ToolName": "a fork must use the new name supplied by --tool-name",
}

var forkOverrideOnlyFields = map[string]string{
	"ClientToken": "an idempotency token must be newly supplied, never copied from the source tool",
}

func (f *fakeControlPlane) GetTool(_ context.Context, toolID string) (apivalue.Object, error) {
	f.getIDs = append(f.getIDs, toolID)
	if f.getErr != nil {
		return nil, f.getErr
	}
	if toolID == "sdt-new" {
		status := f.newStatus
		if status == "" {
			status = "ACTIVE"
		}
		return apivalue.Decode(&ags.SandboxTool{ToolId: &toolID, Status: &status})
	}
	if f.sourceTool != nil {
		return apivalue.Decode(f.sourceTool)
	}
	return apivalue.Decode(sourceTool(toolID))
}

func (f *fakeControlPlane) Call(_ context.Context, action string, request map[string]any) (any, error) {
	f.action = action
	f.request = request
	f.callCount++
	if f.callErr != nil {
		return nil, f.callErr
	}
	return map[string]any{"ToolId": "sdt-new"}, nil
}

func TestModuleWaitsForForkedToolWithoutRepeatingCreate(t *testing.T) {
	cp := &fakeControlPlane{}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp, Values: map[string]any{
		resourcewait.OptionsKey: resourcewait.Options{Interval: time.Millisecond, Timeout: 50 * time.Millisecond},
	}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Args: []string{"sdt-source"},
		Flags: map[string]command.FlagValue{
			"tool-name": {Name: "tool-name", Type: command.FlagString, String: "copy", Changed: true},
			"wait":      {Name: "wait", Type: command.FlagBool, Bool: true},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cp.callCount != 1 || len(cp.getIDs) != 2 || cp.getIDs[0] != "sdt-source" || cp.getIDs[1] != "sdt-new" {
		t.Fatalf("Call = %d, GetTool ids = %#v", cp.callCount, cp.getIDs)
	}
	if result.Data.(map[string]any)["Status"] != "ACTIVE" {
		t.Fatalf("result = %#v", result.Data)
	}
}

func TestModuleWaitReportsForkedToolIsolatedAsUnavailable(t *testing.T) {
	cp := &fakeControlPlane{newStatus: "ISOLATED"}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp, Values: map[string]any{
		resourcewait.OptionsKey: resourcewait.Options{Interval: time.Millisecond, Timeout: 50 * time.Millisecond},
	}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{
		Args: []string{"sdt-source"},
		Flags: map[string]command.FlagValue{
			"tool-name": {Name: "tool-name", Type: command.FlagString, String: "copy", Changed: true},
			"wait":      {Name: "wait", Type: command.FlagBool, Bool: true},
		},
	})
	var cliErr *output.CLIError
	if !errors.As(err, &cliErr) || cliErr.Failure.Code != "WAIT_PREEMPTED" {
		t.Fatalf("error = %#v, want WAIT_PREEMPTED", err)
	}
	if cp.callCount != 1 {
		t.Fatalf("Call = %d, want exactly one create mutation", cp.callCount)
	}
}

func TestModuleCopiesCreateCapableFields(t *testing.T) {
	source := sourceTool("sdt-source")
	cp := &fakeControlPlane{sourceTool: source}
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
	module := Module()
	api, ok := module.Descriptor.API.(apicli.APIDescriptor)
	if !ok {
		t.Fatalf("tool.fork descriptor API = %T, want apicli.APIDescriptor", module.Descriptor.API)
	}
	expectedInherited := baseCreateRequestFromTool(source)
	descriptorFields := map[string]bool{}
	for _, field := range api.Fields {
		descriptorFields[field.Name] = true
		if _, replaced := forkReplacementFields[field.Name]; replaced {
			continue
		}
		if _, overrideOnly := forkOverrideOnlyFields[field.Name]; overrideOnly {
			continue
		}
		for _, input := range field.Inputs {
			if input.SendDefault {
				t.Fatalf("inherited descriptor field %s uses SendDefault and can bypass source-copy coverage", field.Name)
			}
		}
		want, copied := expectedInherited[field.Name]
		if !copied {
			t.Fatalf("source-copy policy missing inherited descriptor field %s", field.Name)
		}
		got, ok := cp.request[field.Name]
		if !ok {
			t.Fatalf("request missing copied descriptor field %s: %#v", field.Name, cp.request)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("copied descriptor field %s = %#v, want source-derived %#v", field.Name, got, want)
		}
	}
	checkPolicy := func(fieldName, reason string) {
		t.Helper()
		if strings.TrimSpace(reason) == "" {
			t.Errorf("non-inherited descriptor field %s must include a reason", fieldName)
		}
		if !descriptorFields[fieldName] {
			t.Errorf("non-inherited field %s is not present in the fork API descriptor", fieldName)
		}
	}
	for _, fieldName := range slices.Sorted(maps.Keys(forkReplacementFields)) {
		checkPolicy(fieldName, forkReplacementFields[fieldName])
		if _, duplicate := forkOverrideOnlyFields[fieldName]; duplicate {
			t.Errorf("field %s cannot be both replacement and override-only", fieldName)
		}
		if _, ok := cp.request[fieldName]; !ok {
			t.Errorf("replacement field %s is missing from the create request", fieldName)
		}
	}
	for _, fieldName := range slices.Sorted(maps.Keys(forkOverrideOnlyFields)) {
		checkPolicy(fieldName, forkOverrideOnlyFields[fieldName])
		if _, ok := cp.request[fieldName]; ok {
			t.Errorf("override-only field %s was sent without an explicit override", fieldName)
		}
	}
	for _, key := range []string{"ToolId", "Status", "StatusReason", "CreateTime", "UpdateTime"} {
		if _, ok := cp.request[key]; ok {
			t.Fatalf("request copied excluded field %s: %#v", key, cp.request)
		}
	}
	if cp.request["DefaultTimeout"] != "300s" {
		t.Fatalf("DefaultTimeout = %#v", cp.request["DefaultTimeout"])
	}
	custom, _ := apivalue.Decode(cp.request["CustomConfiguration"])
	if custom.String("ImageRegistryType") != "enterprise" {
		t.Fatalf("custom = %#v", custom)
	}
	computer, _ := apivalue.Decode(cp.request["ComputerConfiguration"])
	if computer.Object("WAAConfiguration").String("ImageId") != "img-source" {
		t.Fatalf("computer = %#v", computer)
	}
	if computer.Object("OSWorldConfiguration").String("Version") != "osworld2" {
		t.Fatalf("computer = %#v", computer)
	}

}

func TestModuleAppliesExplicitOverrides(t *testing.T) {
	cp := &fakeControlPlane{}
	runFork(t, cp, command.Request{
		Args: []string{"sdt-source"},
		Flags: map[string]command.FlagValue{
			"tool-name":              {Name: "tool-name", Type: command.FlagString, String: "copy", Changed: true},
			"tool-type":              {Name: "tool-type", Type: command.FlagString, String: "custom", Changed: true},
			"description":            {Name: "description", Type: command.FlagString, String: "", Changed: true},
			"default-timeout":        {Name: "default-timeout", Type: command.FlagString, String: "1h", Changed: true},
			"network-configuration":  {Name: "network-configuration", Type: command.FlagString, String: `{"NetworkMode":"PUBLIC"}`, Changed: true},
			"tags":                   {Name: "tags", Type: command.FlagString, String: `[{"Key":"team","Value":"qa"}]`, Changed: true},
			"computer-configuration": {Name: "computer-configuration", Type: command.FlagString, String: `{"WAAConfiguration":{"ImageId":"img-override"},"OSWorldConfiguration":{"Version":"osworld1"}}`, Changed: true},
			"role-arn":               {Name: "role-arn", Type: command.FlagString, String: "", Changed: true},
			"client-token":           {Name: "client-token", Type: command.FlagString, String: "tok", Changed: true},
			"persistent":             {Name: "persistent", Type: command.FlagBool, Bool: false, Changed: true},
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
	computer := cp.request["ComputerConfiguration"].(map[string]any)
	waa := computer["WAAConfiguration"].(map[string]any)
	if waa["ImageId"] != "img-override" {
		t.Fatalf("ComputerConfiguration = %#v", computer)
	}
	osWorld := computer["OSWorldConfiguration"].(map[string]any)
	if osWorld["Version"] != "osworld1" {
		t.Fatalf("ComputerConfiguration = %#v", computer)
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
	tags := cp.request["Tags"].([]map[string]any)
	if len(tags) != 1 || tags[0]["Key"] != "env" {
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
	waaImageID := "img-source"
	osWorldVersion := "osworld2"
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
		ComputerConfiguration: &ags.ComputerConfiguration{
			WAAConfiguration:     &ags.WAAConfiguration{ImageId: &waaImageID},
			OSWorldConfiguration: &ags.OSWorldConfiguration{Version: &osWorldVersion},
		},
		LogConfiguration: &ags.LogConfiguration{LogSources: &ags.LogSources{Files: []*string{&logFile}}},
		StatusReason:     &statusReason,
	}
}

func strPtr(value string) *string {
	return &value
}

var _ ControlPlane = (*fakeControlPlane)(nil)
var _ apicli.ControlPlane = (*fakeControlPlane)(nil)

// Mutate every existing field, not only a newly added fixture field. This catches
// wrappers that accidentally freeze any current parser or flag definition.
func TestForkInheritsCurrentCreateFields(t *testing.T) {
	base := toolcreate.APIDescriptor()
	for index, original := range base.Fields {
		t.Run(original.Name, func(t *testing.T) {
			create := toolcreate.APIDescriptor()
			field := &create.Fields[index]
			field.Parser = "common.default_json"
			field.Required = true
			field.Inputs = []apicli.InputSpec{{Name: "replacement", Flag: "replacement", Type: command.FlagString, Usage: "Changed contract", Default: `{"default":true}`, SendDefault: true}}
			fork := forkDescriptor(create)
			inherited := fork.Fields[index]
			if inherited.Parser != field.Parser || inherited.Inputs[0].Flag != "replacement" || inherited.Inputs[0].Usage != "Changed contract" || inherited.Required != (original.Name == "ToolName") {
				t.Fatalf("stale fork field: %+v", inherited)
			}
			builder := apicli.NewRequestBuilder(apicli.APIDescriptor{Fields: []apicli.FieldSpec{inherited}})
			request, err := builder.Build(command.Request{Flags: map[string]command.FlagValue{"replacement": {String: `{"value":true}`, Changed: true, Type: command.FlagString}}})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(request[original.Name], map[string]any{"value": true}) {
				t.Fatalf("parser did not follow contract: %#v", request)
			}
			if inherited.Inputs[0].Default != nil || inherited.Inputs[0].SendDefault {
				t.Fatal("fork default would overwrite source values")
			}
			if create.Fields[index].Inputs[0].Default == nil {
				t.Fatal("fork mutated create descriptor")
			}
			create.Fields = slices.Delete(create.Fields, index, index+1)
			for _, retained := range forkDescriptor(create).Fields {
				if retained.Name == original.Name {
					t.Fatal("fork resurrected excluded field")
				}
			}
		})
	}
}

func TestEmptyStringOverridesFollowCurrentParser(t *testing.T) {
	fields := []apicli.FieldSpec{
		{Name: "Description", Parser: "common.default_json", Inputs: []apicli.InputSpec{{Flag: "description"}}},
		{Name: "NewString", Parser: "common.default_string", Inputs: []apicli.InputSpec{{Flag: "renamed-string"}}},
	}
	overrides := map[string]any{}
	applyExplicitEmptyStringOverrides(overrides, command.Request{Flags: map[string]command.FlagValue{
		"description": {Changed: true}, "renamed-string": {Changed: true}, "role-arn": {Changed: true},
	}}, fields)
	if !reflect.DeepEqual(overrides, map[string]any{"NewString": ""}) {
		t.Fatalf("stale empty string overrides: %#v", overrides)
	}
}

package list

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

type fakeMixedControlPlane struct {
	action   string
	request  map[string]any
	response any
}

func (f *fakeMixedControlPlane) Call(_ context.Context, action string, request map[string]any) (any, error) {
	f.action = action
	f.request = request
	if f.response != nil {
		return f.response, nil
	}
	return map[string]any{"ok": true}, nil
}

func TestModuleBuildsAndRendersToolList(t *testing.T) {
	id := "sdt-unit"
	name := "tool"
	toolType := "code-interpreter"
	status := "ACTIVE"
	desc := "a useful tool"
	created := "2026-05-21T10:00:00Z"
	total := int64(2)
	network := "PUBLIC"
	tagKey := "env"
	tagValue := "unit"
	waaImageID := "img-unit"
	cp := &fakeMixedControlPlane{response: &ags.DescribeSandboxToolListResponseParams{
		TotalCount: &total,
		SandboxToolSet: []*ags.SandboxTool{{
			ToolId:      &id,
			ToolName:    &name,
			ToolType:    &toolType,
			Status:      &status,
			Description: &desc,
			CreateTime:  &created,
			Tags:        []*ags.Tag{{Key: &tagKey, Value: &tagValue}},
			NetworkConfiguration: &ags.NetworkConfiguration{
				NetworkMode: &network,
			},
			ComputerConfiguration: &ags.ComputerConfiguration{
				WAAConfiguration: &ags.WAAConfiguration{ImageId: &waaImageID},
			},
		}},
	}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{Flags: validFlags()})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if cp.action != "DescribeSandboxToolList" || cp.request["Limit"] != 20 {
		t.Fatalf("action=%q request=%#v", cp.action, cp.request)
	}
	data := result.Data.(map[string]any)
	items := data["Items"].([]map[string]any)
	if len(items) != 1 || items[0]["ToolId"] != id {
		t.Fatalf("items = %#v", items)
	}
	computer, ok := items[0]["ComputerConfiguration"].(*ags.ComputerConfiguration)
	if !ok || computer.WAAConfiguration == nil || computer.WAAConfiguration.ImageId == nil || *computer.WAAConfiguration.ImageId != waaImageID {
		t.Fatalf("ComputerConfiguration = %#v", items[0]["ComputerConfiguration"])
	}
	var text bytes.Buffer
	result.Text(&text)
	if !strings.Contains(text.String(), "ID") || !strings.Contains(text.String(), "env=unit") {
		t.Fatalf("text = %q", text.String())
	}
}

func TestModuleRendersEmptyToolList(t *testing.T) {
	total := int64(0)
	cp := &fakeMixedControlPlane{response: &ags.DescribeSandboxToolListResponseParams{TotalCount: &total}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{Flags: validFlags()})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	var text bytes.Buffer
	result.Text(&text)
	if !strings.Contains(text.String(), "No tools found") {
		t.Fatalf("text = %q", text.String())
	}
}

func TestModuleUsesDefaultLimitWhenUnset(t *testing.T) {
	total := int64(0)
	cp := &fakeMixedControlPlane{response: &ags.DescribeSandboxToolListResponseParams{TotalCount: &total}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Flags: map[string]command.FlagValue{
			"offset": {Name: "offset", Type: command.FlagInt, Int: 0},
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if _, ok := cp.request["Limit"]; ok {
		t.Fatalf("Limit = %#v, want omitted", cp.request["Limit"])
	}
	pagination := result.Data.(map[string]any)["Pagination"].(map[string]any)
	if _, ok := pagination["Limit"]; ok {
		t.Fatalf("Pagination limit = %#v, want omitted", pagination["Limit"])
	}
}

func TestModuleAllowsZeroLimit(t *testing.T) {
	total := int64(0)
	cp := &fakeMixedControlPlane{response: &ags.DescribeSandboxToolListResponseParams{TotalCount: &total}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Flags: map[string]command.FlagValue{
			"offset": {Name: "offset", Type: command.FlagInt, Int: 0, Changed: true},
			"limit":  {Name: "limit", Type: command.FlagInt, Int: 0, Changed: true},
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got := cp.request["Limit"]; got != 0 {
		t.Fatalf("Limit = %#v, want 0", got)
	}
	pagination := result.Data.(map[string]any)["Pagination"].(map[string]any)
	if got := pagination["Limit"]; got != 0 {
		t.Fatalf("Pagination limit = %#v, want 0", got)
	}
}

func TestModuleRejectsInvalidFilters(t *testing.T) {
	runtime, err := Module().Build(command.Deps{ControlPlane: &fakeMixedControlPlane{}})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	for _, tc := range []struct {
		name  string
		flags map[string]command.FlagValue
		want  string
	}{
		{name: "offset", flags: withFlag(validFlags(), "offset", command.FlagValue{Name: "offset", Type: command.FlagInt, Int: -1, Changed: true}), want: "offset"},
		{name: "limit", flags: withFlag(validFlags(), "limit", command.FlagValue{Name: "limit", Type: command.FlagInt, Int: -1, Changed: true}), want: "limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runtime.Handler.Run(context.Background(), command.Request{Flags: tc.flags})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestModuleSkipsPaginationValidationForRequestFlag(t *testing.T) {
	total := int64(0)
	cp := &fakeMixedControlPlane{response: &ags.DescribeSandboxToolListResponseParams{TotalCount: &total}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{
		Flags: map[string]command.FlagValue{
			"request": {Name: "request", Type: command.FlagString, String: `{"Offset":1,"Limit":5}`, Changed: true},
			"offset":  {Name: "offset", Type: command.FlagInt, Int: -1},
			"limit":   {Name: "limit", Type: command.FlagInt, Int: -1},
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got := cp.request["Offset"]; got != float64(1) {
		t.Fatalf("Offset = %#v, want raw request value", got)
	}
	if got := cp.request["Limit"]; got != float64(5) {
		t.Fatalf("Limit = %#v, want raw request value", got)
	}
}

func TestRenderToolListSortsTagsAndFormatsCreatedTime(t *testing.T) {
	tagKeyA := "alpha"
	tagValueA := "1"
	tagKeyZ := "zeta"
	tagValueZ := "2"
	created := "2026-05-21T10:00:00Z"
	total := int64(1)
	toolID := "sdt-unit"
	toolName := "demo"
	toolType := "code-interpreter"
	status := "ACTIVE"
	response := &ags.DescribeSandboxToolListResponseParams{
		TotalCount: &total,
		SandboxToolSet: []*ags.SandboxTool{{
			ToolId:     &toolID,
			ToolName:   &toolName,
			ToolType:   &toolType,
			Status:     &status,
			CreateTime: &created,
			Tags:       []*ags.Tag{{Key: &tagKeyZ, Value: &tagValueZ}, {Key: &tagKeyA, Value: &tagValueA}},
		}},
	}
	var text bytes.Buffer
	renderToolList(&text, response)
	got := text.String()
	if !strings.Contains(got, "alpha=1,zeta=2") {
		t.Fatalf("text = %q", got)
	}
	if !strings.Contains(got, "05-21 10:00") {
		t.Fatalf("text = %q", got)
	}
}

func validFlags() map[string]command.FlagValue {
	return map[string]command.FlagValue{
		"offset": {Name: "offset", Type: command.FlagInt, Int: 0, Changed: true},
		"limit":  {Name: "limit", Type: command.FlagInt, Int: 20, Changed: true},
	}
}

func withFlag(flags map[string]command.FlagValue, name string, value command.FlagValue) map[string]command.FlagValue {
	out := map[string]command.FlagValue{}
	for k, v := range flags {
		out[k] = v
	}
	out[name] = value
	return out
}

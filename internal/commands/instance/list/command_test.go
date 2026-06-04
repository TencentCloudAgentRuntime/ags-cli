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
	action    string
	request   map[string]any
	requests  []map[string]any
	response  any
	responses []any
}

func (f *fakeMixedControlPlane) Call(_ context.Context, action string, request map[string]any) (any, error) {
	f.action = action
	f.request = request
	captured := map[string]any{}
	for key, value := range request {
		captured[key] = value
	}
	f.requests = append(f.requests, captured)
	if len(f.responses) > 0 {
		response := f.responses[0]
		f.responses = f.responses[1:]
		return response, nil
	}
	if f.response != nil {
		return f.response, nil
	}
	return map[string]any{"ok": true}, nil
}

func TestModuleBuildsAndRendersInstanceList(t *testing.T) {
	id := "ins-unit"
	toolName := "tool"
	status := "RUNNING"
	created := "2026-05-21T10:00:00Z"
	expires := "2026-05-21T11:00:00Z"
	timeout := uint64(300)
	total := int64(2)
	mountName := "workspace"
	cp := &fakeMixedControlPlane{response: &ags.DescribeSandboxInstanceListResponseParams{
		TotalCount: &total,
		InstanceSet: []*ags.SandboxInstance{{
			InstanceId:     &id,
			ToolName:       &toolName,
			Status:         &status,
			CreateTime:     &created,
			ExpiresAt:      &expires,
			TimeoutSeconds: &timeout,
			MountOptions:   []*ags.MountOption{{Name: &mountName}},
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
	if cp.action != "DescribeSandboxInstanceList" || cp.request["Limit"] != 20 {
		t.Fatalf("action=%q request=%#v", cp.action, cp.request)
	}
	data := result.Data.(map[string]any)
	items := data["Items"].([]map[string]any)
	if len(items) != 1 || items[0]["InstanceId"] != id {
		t.Fatalf("items = %#v", items)
	}
	var text bytes.Buffer
	result.Text(&text)
	if !strings.Contains(text.String(), id) || !strings.Contains(text.String(), "workspace") {
		t.Fatalf("text = %q", text.String())
	}
}

func TestModuleRendersEmptyInstanceList(t *testing.T) {
	total := int64(0)
	cp := &fakeMixedControlPlane{response: &ags.DescribeSandboxInstanceListResponseParams{TotalCount: &total}}
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
	if !strings.Contains(text.String(), "No instances found") {
		t.Fatalf("text = %q", text.String())
	}
}

func TestModuleUsesDefaultLimitWhenUnset(t *testing.T) {
	total := int64(0)
	cp := &fakeMixedControlPlane{response: &ags.DescribeSandboxInstanceListResponseParams{TotalCount: &total}}
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
	cp := &fakeMixedControlPlane{response: &ags.DescribeSandboxInstanceListResponseParams{TotalCount: &total}}
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

func TestModuleListAllFetchesEveryPage(t *testing.T) {
	total := int64(allPageLimit + 1)
	firstPage := make([]*ags.SandboxInstance, allPageLimit)
	for i := range firstPage {
		id := "ins-page-1"
		firstPage[i] = &ags.SandboxInstance{InstanceId: &id}
	}
	secondID := "ins-page-2"
	cp := &fakeMixedControlPlane{responses: []any{
		&ags.DescribeSandboxInstanceListResponseParams{
			TotalCount:  &total,
			InstanceSet: firstPage,
		},
		&ags.DescribeSandboxInstanceListResponseParams{
			TotalCount:  &total,
			InstanceSet: []*ags.SandboxInstance{{InstanceId: &secondID}},
		},
	}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{Flags: withFlag(validFlagsWithoutPagination(), "all", command.FlagValue{Name: "all", Type: command.FlagBool, Bool: true, Changed: true})})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(cp.requests) != 2 {
		t.Fatalf("requests = %#v, want 2 calls", cp.requests)
	}
	if cp.requests[0]["Offset"] != 0 || cp.requests[0]["Limit"] != allPageLimit {
		t.Fatalf("first request = %#v", cp.requests[0])
	}
	if cp.requests[1]["Offset"] != allPageLimit || cp.requests[1]["Limit"] != allPageLimit {
		t.Fatalf("second request = %#v", cp.requests[1])
	}
	items := result.Data.(map[string]any)["Items"].([]map[string]any)
	if len(items) != allPageLimit+1 || items[allPageLimit]["InstanceId"] != secondID {
		t.Fatalf("items = %#v", items)
	}
	if items[0]["Region"] == "" {
		t.Fatalf("Region not set in item: %#v", items[0])
	}
	var text bytes.Buffer
	result.Text(&text)
	if !strings.Contains(text.String(), "REGION") || !strings.Contains(text.String(), secondID) {
		t.Fatalf("text = %q", text.String())
	}
}

func TestModuleRejectsInvalidListFlags(t *testing.T) {
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
		{name: "all with offset", flags: withFlag(withFlag(validFlags(), "all", command.FlagValue{Name: "all", Type: command.FlagBool, Bool: true, Changed: true}), "offset", command.FlagValue{Name: "offset", Type: command.FlagInt, Int: 1, Changed: true}), want: "--all"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runtime.Handler.Run(context.Background(), command.Request{Flags: tc.flags})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func validFlagsWithoutPagination() map[string]command.FlagValue {
	return map[string]command.FlagValue{
		"offset": {Name: "offset", Type: command.FlagInt, Int: 0},
		"limit":  {Name: "limit", Type: command.FlagInt, Int: 0},
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

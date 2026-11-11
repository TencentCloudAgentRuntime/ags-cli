package list

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
)

type fakeControlPlane struct {
	action   string
	request  map[string]any
	response any
	err      error
}

func (f *fakeControlPlane) Call(_ context.Context, action string, request map[string]any) (any, error) {
	f.action, f.request = action, request
	return f.response, f.err
}

func TestListRequestAndPresentationPreserveWireData(t *testing.T) {
	response := map[string]any{
		"PreCacheImageList": []any{
			map[string]any{"PreCacheImageId": "cache-ready", "Image": "example:latest", "Status": "Success", "ReadyTime": "2026-11-09T22:54:47+08:00", "Future": json.Number("9007199254740993")},
			map[string]any{"PreCacheImageId": "cache-pending", "Image": "other:latest", "Status": "Pending"},
		},
		"NextToken": "next-cursor", "TotalCount": json.Number("9007199254740993"),
	}
	cp := &fakeControlPlane{response: response}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Handler.Run(t.Context(), command.Request{Flags: map[string]command.FlagValue{
		"filters":          {Name: "filters", Type: command.FlagString, String: `[{"Name":"status","Values":["Success"]}]`, Changed: true},
		"max-results":      {Name: "max-results", Type: command.FlagInt, Int: 20, Changed: true},
		"next-token":       {Name: "next-token", Type: command.FlagString, String: "previous-cursor", Changed: true},
		"need-total-count": {Name: "need-total-count", Type: command.FlagBool, Bool: false, Changed: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"Filters": []any{map[string]any{"Name": "status", "Values": []any{"Success"}}}, "MaxResults": 20, "NextToken": "previous-cursor", "NeedTotalCount": false}
	if cp.action != "DescribePreCacheImageList" || !reflect.DeepEqual(cp.request, want) {
		t.Fatalf("action=%s request=%v", cp.action, cp.request)
	}
	if !reflect.DeepEqual(result.Data, response) {
		t.Fatalf("wire data changed: %v", result.Data)
	}
	var text bytes.Buffer
	result.Text(&text)
	for _, value := range []string{"READY TIME", "cache-ready", "cache-pending", "2026-11-09T22:54:47+08:00", "Pending", "-", "Total: 9007199254740993", "Next token: next-cursor"} {
		if !strings.Contains(text.String(), value) {
			t.Errorf("text missing %q: %s", value, text.String())
		}
	}
}

func TestListEmptyAndFailureResponses(t *testing.T) {
	cause := errors.New("service unavailable")
	for _, tc := range []struct {
		name     string
		response any
		err      error
		wantErr  bool
	}{
		{"empty", map[string]any{"PreCacheImageList": []any{}, "NextToken": ""}, nil, false},
		{"bad array", map[string]any{"PreCacheImageList": "invalid"}, nil, true},
		{"null item", map[string]any{"PreCacheImageList": []any{nil}}, nil, true},
		{"missing response", nil, nil, true},
		{"service error", nil, cause, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cp := &fakeControlPlane{response: tc.response, err: tc.err}
			runtime, err := Module().Build(command.Deps{ControlPlane: cp})
			if err != nil {
				t.Fatal(err)
			}
			result, err := runtime.Handler.Run(t.Context(), command.Request{})
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v, expected failure=%v", err, tc.wantErr)
			}
			if tc.err != nil && !errors.Is(err, cause) {
				t.Fatalf("service error lost: %v", err)
			}
			if err == nil {
				var text bytes.Buffer
				result.Text(&text)
				if !strings.Contains(text.String(), "No image pre-cache tasks found") || strings.Contains(text.String(), "Total:") || strings.Contains(text.String(), "Next token:") {
					t.Fatalf("empty list text: %s", text.String())
				}
			}
		})
	}
}

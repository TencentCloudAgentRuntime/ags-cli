package lookup

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
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

func positionalRequest(ids ...string) command.Request {
	request := command.Request{Args: ids, ArgValues: map[string]string{}}
	if len(ids) > 0 {
		request.ArgValues["instance-ids"] = ids[0]
	}
	return request
}

func TestLookupUsesPublishedContract(t *testing.T) {
	api := APIDescriptor()
	catalog, err := apimeta.Get()
	if err != nil {
		t.Fatal(err)
	}
	action, ok := catalog.Spec.Actions["DescribeSandboxInstances"]
	if !ok || api.API.Action != action.Name || api.API.RequestType != action.Input || api.API.ResponseType != action.Output {
		t.Fatalf("lookup API identity differs from published contract: %+v", api.API)
	}
	fields := map[string]bool{}
	for _, field := range api.Fields {
		fields[field.Name] = true
	}
	for _, member := range catalog.Spec.Object(action.Input).Members {
		if !member.Disabled && !fields[member.Name] {
			t.Errorf("lookup omits published field %s", member.Name)
		}
		delete(fields, member.Name)
	}
	if len(fields) != 0 {
		t.Errorf("lookup has unknown fields: %v", fields)
	}
	module := Module()
	if module.Descriptor.Generated == nil || module.Descriptor.Source != command.SourceMixedAPI || !module.Descriptor.Spec.Args[0].Repeatable {
		t.Fatal("lookup lost its generated contract or variadic positional IDs")
	}
}

func TestLookupPreservesRequestAndResponse(t *testing.T) {
	response := map[string]any{"InstanceSet": []any{
		map[string]any{"InstanceId": "ins-b", "ToolId": "tool-b", "Status": "PAUSED", "Extension": nil},
		map[string]any{"InstanceId": "ins-a", "ToolId": "tool-a", "Status": "RUNNING"},
	}, "RequestId": "request-test"}
	cp := &fakeControlPlane{response: response}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Handler.Run(t.Context(), positionalRequest("ins-b", "ins-a", "ins-b"))
	if err != nil {
		t.Fatal(err)
	}
	if cp.action != "DescribeSandboxInstances" || !reflect.DeepEqual(cp.request, map[string]any{"InstanceIds": []string{"ins-b", "ins-a", "ins-b"}}) {
		t.Fatalf("unexpected call: %s %#v", cp.action, cp.request)
	}
	if !reflect.DeepEqual(result.Data, response) || result.MetaExtra["Action"] != cp.action {
		t.Fatalf("response or metadata changed: %#v", result)
	}
	var text bytes.Buffer
	result.Text(&text)
	first, second := strings.Index(text.String(), "ins-b"), strings.Index(text.String(), "ins-a")
	if first < 0 || second < 0 || first >= second || !strings.Contains(text.String(), "PAUSED") {
		t.Fatalf("text lost response order or status: %s", &text)
	}
}

func TestLookupEmptyResponseIsSuccessful(t *testing.T) {
	cp := &fakeControlPlane{response: map[string]any{"InstanceSet": []any{}}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Handler.Run(t.Context(), positionalRequest("ins-missing"))
	if err != nil {
		t.Fatal(err)
	}
	var text bytes.Buffer
	result.Text(&text)
	if text.String() != "No instances found\n" || result.Failure != nil {
		t.Fatalf("empty lookup failed: %#v %q", result, &text)
	}
}

func TestLookupAcceptsIDBoundaries(t *testing.T) {
	for _, ids := range [][]string{{"ins-a"}, {strings.Repeat("a", 64)}, slices.Repeat([]string{"ins-a"}, 100)} {
		cp := &fakeControlPlane{response: map[string]any{"InstanceSet": []any{}}}
		runtime, err := Module().Build(command.Deps{ControlPlane: cp})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runtime.Handler.Run(t.Context(), positionalRequest(ids...)); err != nil {
			t.Fatalf("valid %d-ID lookup rejected: %v", len(ids), err)
		}
		if !reflect.DeepEqual(cp.request["InstanceIds"], ids) {
			t.Fatalf("IDs changed before service validation: %#v", cp.request)
		}
	}
}

func TestLookupRejectsMalformedResponsesAndPreservesErrors(t *testing.T) {
	for _, response := range []any{
		nil, "invalid", map[string]any{}, map[string]any{"InstanceSet": nil},
		map[string]any{"InstanceSet": "invalid"}, map[string]any{"InstanceSet": []any{nil}},
		map[string]any{"InstanceSet": []any{map[string]any{}}},
		map[string]any{"InstanceSet": []any{map[string]any{"InstanceId": 1}}},
	} {
		cp := &fakeControlPlane{response: response}
		runtime, err := Module().Build(command.Deps{ControlPlane: cp})
		if err != nil {
			t.Fatal(err)
		}
		if result, err := runtime.Handler.Run(t.Context(), positionalRequest("ins-a")); err == nil || result != nil {
			t.Fatalf("malformed response succeeded: %#v, %v", result, err)
		}
	}
	want := errors.New("control plane unavailable")
	runtime, err := Module().Build(command.Deps{ControlPlane: &fakeControlPlane{err: want}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Handler.Run(t.Context(), positionalRequest("ins-a")); !errors.Is(err, want) {
		t.Fatalf("control-plane error lost: %v", err)
	}
}

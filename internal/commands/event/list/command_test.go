package list

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

const eventJSON = `{"EventId":"event-one","Source":"Sandbox","ObjectType":"SandboxInstance","ObjectId":"instance-one","EventLevel":"Warning","EventType":"SandboxStopping","Message":"Stopping","Reason":"Manual","Extensions":"{\"ToolId\":\"tool-one\"}","OccurredAt":"2026-11-09T22:54:47.123456789+08:00","Future":9007199254740993}`

type fakeControlPlane struct {
	calls    int
	action   string
	request  map[string]any
	response any
	err      error
}

func (f *fakeControlPlane) Call(_ context.Context, action string, request map[string]any) (any, error) {
	f.calls++
	f.action, f.request = action, request
	return f.response, f.err
}

func object(t *testing.T, raw string) map[string]any {
	t.Helper()
	var value map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func response(t *testing.T) map[string]any {
	t.Helper()
	return object(t, `{"Events":[`+eventJSON+`],"TotalCount":9007199254740993,"RequestId":"request-one","StartTime":"2026-11-09T22:00:00+08:00","EndTime":"2026-11-09T23:00:00.123456789+08:00","Future":9007199254740993}`)
}

func run(t *testing.T, cp *fakeControlPlane, req command.Request) (*command.Result, error) {
	t.Helper()
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatal(err)
	}
	return runtime.Handler.Run(t.Context(), req)
}

func request(raw string) command.Request {
	return command.Request{Flags: map[string]command.FlagValue{
		"request": {Name: "request", Type: command.FlagString, Changed: true, String: raw},
	}}
}

func TestQueryRequestAndPresentationPreserveWireData(t *testing.T) {
	for _, mode := range []string{"Raw", "Aggregate"} {
		for _, effectiveMode := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/effective=%t", mode, effectiveMode), func(t *testing.T) {
				wire := response(t)
				if effectiveMode {
					wire["Mode"] = mode
				}
				item := wire["Events"].([]any)[0].(map[string]any)
				if mode == "Aggregate" {
					delete(item, "OccurredAt")
					item["Count"] = json.Number("9007199254740993")
					item["FirstOccurredAt"] = "2026-11-09T22:00:01.123456789+08:00"
					item["LastOccurredAt"] = "2026-11-09T22:54:47.123456789+08:00"
				}
				query := `{"Filters":[{"Name":"ObjectId","Values":["instance-one"]}],"StartTime":"2026-11-09T22:00:00+08:00","EndTime":"2026-11-09T23:00:00.123456789+08:00","Offset":0,"Limit":100,"Order":"Asc","Mode":"` + mode + `"}`
				cp := &fakeControlPlane{response: wire}
				result, err := run(t, cp, request(query))
				if err != nil {
					t.Fatal(err)
				}
				if cp.action != "DescribeEventCenterEvents" || !reflect.DeepEqual(cp.request, object(t, query)) {
					t.Fatalf("action=%s request=%v", cp.action, cp.request)
				}
				if !reflect.DeepEqual(result.Data, wire) {
					t.Fatal("wire data was projected or normalized")
				}
				before, _ := json.Marshal(wire)
				var text bytes.Buffer
				result.Text(&text)
				after, _ := json.Marshal(result.Data)
				if !bytes.Equal(before, after) {
					t.Fatalf("text rendering mutated JSON: %s", after)
				}
				for _, want := range []string{"EVENT ID", "OBJECT ID", "EVENT TYPE", "LEVEL", "REASON", "event-one", "instance-one", "SandboxStopping", "Warning", "Manual", "Total: 9007199254740993", "EndTime: 2026-11-09T23:00:00.123456789+08:00"} {
					if !strings.Contains(text.String(), want) {
						t.Errorf("text missing %q: %s", want, text.String())
					}
				}
				if mode == "Aggregate" {
					if !strings.Contains(text.String(), "COUNT") || !strings.Contains(text.String(), "FIRST OCCURRED AT") || !strings.Contains(text.String(), item["FirstOccurredAt"].(string)) {
						t.Fatalf("aggregate table: %s", text.String())
					}
				} else if strings.Contains(text.String(), "COUNT") || !strings.Contains(text.String(), item["OccurredAt"].(string)) {
					t.Fatalf("raw table: %s", text.String())
				}
			})
		}
	}
}

func TestQueryAcceptsBoundariesWithoutFillingDefaults(t *testing.T) {
	for _, query := range []string{
		`{}`, `{"Offset":0}`, `{"Offset":10000,"Limit":1}`, `{"Limit":100,"Order":"Desc","Mode":"Raw"}`,
		`{"Offset":1.0,"Limit":1e2}`, `{"Filters":[]}`,
		`{"StartTime":"2000-01-01T00:00:00Z"}`, // Moving retention and partial-window defaults belong to the service.
		`{"EndTime":"2100-01-01T00:00:00Z"}`,
		`{"StartTime":"2026-11-09T11:00:00+08:00","EndTime":"2026-11-09T04:00:00Z"}`,
	} {
		t.Run(query, func(t *testing.T) {
			cp := &fakeControlPlane{response: response(t)}
			if _, err := run(t, cp, request(query)); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cp.request, object(t, query)) {
				t.Fatalf("omitted or explicitly supplied fields changed: %v", cp.request)
			}
		})
	}
	cp := &fakeControlPlane{response: response(t)}
	if _, err := run(t, cp, command.Request{}); err != nil || len(cp.request) != 0 {
		t.Fatalf("default request=%v error=%v", cp.request, err)
	}
	values := make([]string, 20)
	for i := range values {
		values[i] = strings.Repeat("x", 256)
	}
	filters := make([]map[string]any, 0, 10)
	for _, name := range []string{"Source", "ObjectType", "ObjectId", "EventType", "EventLevel", "Reason", "EventId", "ToolId", "ToolType", "Message"} {
		filters = append(filters, map[string]any{"Name": name, "Values": values})
	}
	raw, _ := json.Marshal(map[string]any{"Filters": filters})
	if _, err := run(t, cp, request(string(raw))); err != nil {
		t.Fatalf("valid filter boundaries: %v", err)
	}
}

func TestQueryRejectsInvalidInputsBeforeCallingService(t *testing.T) {
	queries := []string{
		`{"Mode":"raw"}`, `{"Mode":""}`, `{"Mode":null}`, `{"Order":"ASC"}`, `{"Order":false}`,
		`{"Offset":-1}`, `{"Offset":10001}`, `{"Offset":null}`, `{"Limit":0}`, `{"Limit":101}`, `{"Limit":1.5}`, `{"Limit":"10"}`, `{"Limit":9223372036854775808}`,
		`{"StartTime":"bad"}`, `{"StartTime":123}`, `{"EndTime":"2026-11-09T10:00:00"}`,
		`{"StartTime":"2026-11-09T10:00:00Z","EndTime":"2026-11-09T10:00:00Z"}`,
		`{"StartTime":"2026-11-09T11:00:00Z","EndTime":"2026-11-09T10:00:00Z"}`,
		`{"Filters":null}`, `{"Filters":{}}`, `{"Filters":[null]}`, `{"Filters":[{}]}`,
		`{"Filters":[{"Name":"objectid","Values":["a"]}]}`,
		`{"Filters":[{"Name":"ObjectId","Values":[]}]}`,
		`{"Filters":[{"Name":"ObjectId","Values":[""]}]}`,
		`{"Filters":[{"Name":"ObjectId","Values":[null]}]}`,
		`{"Filters":[{"Name":"ObjectId","Values":[1]}]}`,
		`{"Filters":[{"Name":"ObjectId","Values":null}]}`,
		`{"Filters":[{"Name":"ObjectId","Values":["a"],"Extra":true}]}`,
		`{"Filters":[{"Name":"ObjectId","Values":["a"]},{"Name":"ObjectId","Values":["b"]}]}`,
		`{"Unknown":true}`,
	}
	queries = append(queries,
		`{"Filters":[{"Name":"Message","Values":["`+strings.Repeat("界", 86)+`"]}]}`,
		`{"Filters":[{"Name":"Message","Values":["`+strings.Repeat("x", 257)+`"]}]}`,
		`{"Filters":[{"Name":"ObjectId","Values":[`+strings.TrimSuffix(strings.Repeat(`"a",`, 21), ",")+`]}]}`,
		`{"Filters":[`+strings.TrimSuffix(strings.Repeat(`{"Name":"ObjectId","Values":["a"]},`, 13), ",")+`]}`,
	)
	for _, query := range queries {
		t.Run(query, func(t *testing.T) {
			cp := &fakeControlPlane{response: response(t)}
			result, err := run(t, cp, request(query))
			var failure *output.CLIError
			if !errors.As(err, &failure) || failure.ExitCode != 2 || failure.Failure.Kind != "usage" || failure.Failure.Hint == "" {
				t.Fatalf("expected actionable usage failure: %v", err)
			}
			if result != nil || cp.calls != 0 {
				t.Fatalf("invalid request reached service: result=%v calls=%d", result, cp.calls)
			}
		})
	}
}

func TestQueryOptionalFieldsEmptyResultsAndErrors(t *testing.T) {
	for _, mode := range []string{"Raw", "Aggregate"} {
		t.Run(mode, func(t *testing.T) {
			wire := response(t)
			for _, key := range []string{"StartTime", "EndTime"} {
				delete(wire, key)
			}
			item := wire["Events"].([]any)[0].(map[string]any)
			for _, key := range []string{"Reason", "Extensions", "OccurredAt"} {
				delete(item, key)
			}
			cp := &fakeControlPlane{response: wire}
			result, err := run(t, cp, request(`{"Mode":"`+mode+`"}`))
			if err != nil {
				t.Fatal(err)
			}
			var text bytes.Buffer
			result.Text(&text)
			if !reflect.DeepEqual(result.Data, wire) || !strings.Contains(text.String(), "-") || strings.Contains(text.String(), "StartTime:") || strings.Contains(text.String(), "Mode:") {
				t.Fatalf("invented optional fields: %v %s", result.Data, text.String())
			}
		})
	}
	cp := &fakeControlPlane{response: object(t, `{"Events":[],"TotalCount":0,"RequestId":"empty"}`)}
	result, err := run(t, cp, command.Request{})
	if err != nil {
		t.Fatal(err)
	}
	var text bytes.Buffer
	result.Text(&text)
	if text.String() != "No operational events found\nTotal: 0\n" {
		t.Fatalf("empty response: %s", text.String())
	}
	cause := errors.New("service unavailable")
	cp.err = fmt.Errorf("wrapped: %w", cause)
	if result, err := run(t, cp, command.Request{}); result != nil || !errors.Is(err, cause) {
		t.Fatalf("service error lost: %v %v", result, err)
	}
}

func TestQueryRejectsMalformedResponses(t *testing.T) {
	cases := map[string]func(map[string]any){
		"missing events":     func(w map[string]any) { delete(w, "Events") },
		"null events":        func(w map[string]any) { w["Events"] = nil },
		"object events":      func(w map[string]any) { w["Events"] = map[string]any{} },
		"null item":          func(w map[string]any) { w["Events"] = []any{nil} },
		"bad item":           func(w map[string]any) { w["Events"] = []any{"bad"} },
		"missing total":      func(w map[string]any) { delete(w, "TotalCount") },
		"negative total":     func(w map[string]any) { w["TotalCount"] = -1 },
		"bad total":          func(w map[string]any) { w["TotalCount"] = "1" },
		"missing request id": func(w map[string]any) { delete(w, "RequestId") },
		"invalid mode":       func(w map[string]any) { w["Mode"] = "unknown" },
		"bad effective time": func(w map[string]any) { w["EndTime"] = "bad" },
		"missing event id":   func(w map[string]any) { delete(w["Events"].([]any)[0].(map[string]any), "EventId") },
		"invalid extension":  func(w map[string]any) { w["Events"].([]any)[0].(map[string]any)["Extensions"] = map[string]any{} },
		"invalid time":       func(w map[string]any) { w["Events"].([]any)[0].(map[string]any)["OccurredAt"] = "bad" },
		"invalid count":      func(w map[string]any) { w["Events"].([]any)[0].(map[string]any)["Count"] = 0 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			wire := response(t)
			mutate(wire)
			result, err := run(t, &fakeControlPlane{response: wire}, command.Request{})
			if err == nil || result != nil {
				t.Fatalf("malformed response succeeded: %v error=%v", result, err)
			}
		})
	}
	for _, wire := range []any{nil, "bad response"} {
		if result, err := run(t, &fakeControlPlane{response: wire}, command.Request{}); err == nil || result != nil {
			t.Fatalf("missing response succeeded: %v error=%v", result, err)
		}
	}
}

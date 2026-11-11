package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

const eventCenterFilters = `[{"Name":"ObjectId","Values":["instance-one"]}]`
const eventCenterRequest = `{"Filters":[{"Name":"ObjectId","Values":["instance-one"]}],"StartTime":"2026-11-09T22:00:00+08:00","EndTime":"2026-11-09T23:00:00.123456789+08:00","Offset":0,"Limit":20,"Order":"Asc","Mode":"Raw"}`
const eventCenterItem = `{"EventId":"event-one","Source":"Sandbox","ObjectType":"SandboxInstance","ObjectId":"instance-one","EventLevel":"Warning","EventType":"SandboxStopping","Message":"Stopping","Reason":"Manual","Extensions":"{\"ToolId\":\"tool-one\"}","Future":9007199254740993`

// Keep this task-level guard even if the Action has a reasoned raw-only route:
// raw availability alone cannot satisfy the operational query workflow.
func TestEventCenterWorkflowHasResourceCommand(t *testing.T) {
	catalog, err := apimeta.Get()
	if err != nil {
		t.Fatal(err)
	}
	mapped := catalog.Mapping.Actions["DescribeEventCenterEvents"]
	if mapped == nil || mapped.Status != "mapped" || mapped.Command != "event-center.list" || mapped.Request != "DescribeEventCenterEventsRequest" || mapped.Response != "DescribeEventCenterEventsResponse" {
		t.Fatalf("event center workflow must expose event-center.list: %+v", mapped)
	}
	root := contractRoot()
	for _, id := range []string{"event-center", "event-center.list"} {
		cmd, ok := findCobraCommand(root, id)
		if !ok || !strings.Contains(commandHelp(t, cmd), "agr event-center list") {
			t.Fatalf("missing discoverable event center workflow at %s", id)
		}
	}
	if !strings.Contains(commandHelp(t, root), "event-center") {
		t.Fatal("event center group is absent from root help")
	}
	schema := schemaForCommand(t, "event-center.list")
	if !schema.RequiresAuth || !schema.SupportsRequest || !schema.SupportsJSON {
		t.Fatalf("query schema capabilities: %+v", schema)
	}
	request, ok := catalog.Object(mapped.Request)
	if !ok || len(schema.RequestSchema) != len(request.Members) {
		t.Fatal("query schema does not cover all API members")
	}
	for _, member := range request.Members {
		flag := apimeta.KebabCase(member.Name)
		property, ok := schema.RequestSchema[member.Name]
		if !ok || property.CliFlag == nil || *property.CliFlag != flag {
			t.Fatalf("API member %s is missing from schema/flags", member.Name)
		}
	}
	if schema.RequestSchema["Filters"].Type != "array" || schema.Flags["filters"].Type != "json" || !reflect.DeepEqual(schema.Flags["mode"].Values, []string{"Raw", "Aggregate"}) || !reflect.DeepEqual(schema.Flags["order"].Values, []string{"Asc", "Desc"}) {
		t.Fatalf("query schema types or enums drifted: %+v", schema)
	}
}

type eventCenterCLIResult struct {
	stdout, stderr []byte
	code           int
}

func eventCenterCLI(t *testing.T, server *httptest.Server, stdin string, args ...string) eventCenterCLIResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	childArgs := append([]string{"-test.run=^TestTransportLifecycleHelper$", "--", "--non-interactive"}, args...)
	cmd := exec.CommandContext(ctx, os.Args[0], childArgs...)
	home := t.TempDir()
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	cmd.Env = []string{"HOME=" + home, "USERPROFILE=" + home, "PATH=" + os.Getenv("PATH"), "AGR_TRANSPORT_LIFECYCLE_HELPER=success", "GORACE=atexit_sleep_ms=0", "GOCOVERDIR=" + t.TempDir(), "TENCENTCLOUD_SECRET_ID=fake", "TENCENTCLOUD_SECRET_KEY=fake", "AGR_REGION=ap-guangzhou", "AGR_CLOUD_ENDPOINT=" + strings.TrimPrefix(server.URL, "https://"), "AGR_TRANSPORT_TEST_CERT=" + string(cert)}
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exited *exec.ExitError
		if !errors.As(err, &exited) || ctx.Err() != nil {
			t.Fatalf("CLI failed to terminate normally: %v, stderr=%s", err, stderr.String())
		}
		code = exited.ExitCode()
	}
	return eventCenterCLIResult{stdout.Bytes(), stderr.Bytes(), code}
}

func eventCenterObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		t.Fatalf("invalid JSON output: %v: %s", err, raw)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatalf("stdout contains data outside the JSON result: %v", err)
	}
	return object
}

func TestEventCenterSignedTransport(t *testing.T) {
	for _, input := range []string{"flags", "filters file", "filters stdin", "request", "request file", "request stdin", "repeated filters", "defaults", "aggregate", "optional", "empty"} {
		for _, format := range []string{"json", "text"} {
			t.Run(input+"/"+format, func(t *testing.T) {
				requests := make(chan map[string]any, 1)
				var wire string
				filters, stdin := eventCenterFilters, ""
				switch input {
				case "filters file":
					path := filepath.Join(t.TempDir(), "filters.json")
					if err := os.WriteFile(path, []byte(filters), 0600); err != nil {
						t.Fatal(err)
					}
					filters = "@" + path
				case "filters stdin":
					filters, stdin = "-", filters
				}
				args := []string{"event-center", "list", "--filters", filters, "--start-time", "2026-11-09T22:00:00+08:00", "--end-time", "2026-11-09T23:00:00.123456789+08:00", "--offset", "0", "--limit", "20", "--order", "Asc", "--mode", "Raw"}
				wantRequest := eventCenterObject(t, []byte(eventCenterRequest))
				wire = `{"Response":{"Events":[` + eventCenterItem + `,"OccurredAt":"2026-11-09T22:54:47.123456789+08:00"}],"TotalCount":9007199254740993,"RequestId":"query-one","StartTime":"2026-11-09T22:00:00+08:00","EndTime":"2026-11-09T23:00:00.123456789+08:00","Mode":"Raw","Future":9007199254740993}}`
				switch input {
				case "request", "request file", "request stdin":
					value := eventCenterRequest
					switch input {
					case "request file":
						path := filepath.Join(t.TempDir(), "request.json")
						if err := os.WriteFile(path, []byte(value), 0600); err != nil {
							t.Fatal(err)
						}
						value = "@" + path
					case "request stdin":
						value, stdin = "-", value
					}
					args = []string{"event-center", "list", "--request", value}
				case "repeated filters":
					args = append(args, "--filters", "[]", "--filters", eventCenterFilters)
				case "defaults", "empty":
					args, wantRequest = []string{"event-center", "list"}, map[string]any{}
					if input == "empty" {
						wire = `{"Response":{"Events":[],"TotalCount":0,"RequestId":"query-empty"}}`
					}
				case "aggregate":
					args = []string{"event-center", "list", "--mode", "Aggregate"}
					wantRequest = map[string]any{"Mode": "Aggregate"}
					wire = `{"Response":{"Events":[` + eventCenterItem + `,"Count":9007199254740993,"FirstOccurredAt":"2026-11-09T22:00:01+08:00","LastOccurredAt":"2026-11-09T22:54:47.123456789+08:00"}],"TotalCount":1,"RequestId":"query-aggregate","Mode":"Aggregate"}}`
				case "optional":
					args, wantRequest = []string{"event-center", "list", "--mode", "Aggregate"}, map[string]any{"Mode": "Aggregate"}
					wire = `{"Response":{"Events":[` + eventCenterItem + `}],"TotalCount":1,"RequestId":"query-optional"}}`
				}
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("X-TC-Action") != "DescribeEventCenterEvents" || r.Header.Get("Authorization") == "" || r.Header.Get("X-TC-Region") != "ap-guangzhou" {
						t.Error("wrong action/region or unsigned query")
					}
					payload, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
					}
					requests <- eventCenterObject(t, payload)
					_, _ = io.WriteString(w, wire)
				}))
				defer server.Close()
				result := eventCenterCLI(t, server, stdin, append(args, "-o", format)...)
				if result.code != 0 || len(result.stderr) != 0 {
					t.Fatalf("query failed: code=%d %s %s", result.code, result.stdout, result.stderr)
				}
				if format == "json" {
					envelope := eventCenterObject(t, result.stdout)
					want := eventCenterObject(t, []byte(wire))["Response"]
					if envelope["Command"] != "event-center.list" || envelope["Status"] != "succeeded" || !reflect.DeepEqual(envelope["Data"], want) || len(envelope["Warnings"].([]any)) != 0 {
						t.Fatalf("response changed: %s", result.stdout)
					}
				} else {
					for _, expected := range []string{"Total:", "event-one", "instance-one", "SandboxStopping", "Warning", "Manual"} {
						if input == "empty" && expected != "Total:" {
							continue
						}
						if !bytes.Contains(result.stdout, []byte(expected)) {
							t.Errorf("text missing %q: %s", expected, result.stdout)
						}
					}
					if input == "aggregate" && (!bytes.Contains(result.stdout, []byte("COUNT")) || !bytes.Contains(result.stdout, []byte("9007199254740993"))) {
						t.Errorf("aggregate text lost counts: %s", result.stdout)
					}
					if input == "empty" && !bytes.Contains(result.stdout, []byte("No operational events found")) {
						t.Error("missing empty-result message")
					}
				}
				select {
				case payload := <-requests:
					if !reflect.DeepEqual(payload, wantRequest) {
						t.Fatalf("wire request=%v want=%v", payload, wantRequest)
					}
				default:
					t.Fatal("dedicated command did not send a request")
				}
			})
		}
	}
}

func TestEventCenterInvalidCLIInputsDoNotCallService(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = io.WriteString(w, `{}`) }))
	defer server.Close()
	for _, tc := range []struct {
		name, stdin, code string
		args              []string
	}{
		{"enum", "", "INVALID_REQUEST_JSON", []string{"--mode", "raw"}},
		{"order", "", "INVALID_REQUEST_JSON", []string{"--order", "ASC"}},
		{"offset", "", "INVALID_REQUEST_JSON", []string{"--offset", "10001"}},
		{"limit", "", "INVALID_REQUEST_JSON", []string{"--limit", "0"}},
		{"timestamp", "", "INVALID_REQUEST_JSON", []string{"--start-time", "bad"}},
		{"window", "", "INVALID_REQUEST_JSON", []string{"--start-time", "2026-11-09T00:00:00Z", "--end-time", "2026-11-08T00:00:00Z"}},
		{"null", "", "INVALID_REQUEST_JSON", []string{"--filters", "null"}},
		{"filters JSON", "", "INVALID_JSON_FLAG", []string{"--filters", "["}},
		{"closed stdin", "", "INVALID_JSON_FLAG", []string{"--filters", "-"}},
		{"bad stdin", "[", "INVALID_JSON_FLAG", []string{"--filters", "-"}},
		{"missing file", "", "INVALID_REQUEST_INPUT", []string{"--filters", "@" + filepath.Join(t.TempDir(), "missing.json")}},
		{"request type", "", "INVALID_REQUEST_JSON", []string{"--request", `{"Limit":1.5}`}},
		{"request closed stdin", "", "INVALID_REQUEST_JSON", []string{"--request", "-"}},
		{"request bad JSON", "", "INVALID_REQUEST_JSON", []string{"--request", "["}},
		{"request conflict", "", "REQUEST_FLAG_CONFLICT", []string{"--request", "{}", "--mode", "Raw"}},
		{"stdin conflict", "{}", "REQUEST_FLAG_CONFLICT", []string{"--request", "-", "--filters", "-"}},
	} {
		for _, format := range []string{"json", "text"} {
			t.Run(tc.name+"/"+format, func(t *testing.T) {
				args := append([]string{"event-center", "list"}, tc.args...)
				result := eventCenterCLI(t, server, tc.stdin, append(args, "-o", format)...)
				if result.code != 2 {
					t.Fatalf("expected usage exit 2, got %d: %s %s", result.code, result.stdout, result.stderr)
				}
				if format == "json" {
					envelope := eventCenterObject(t, result.stdout)
					failure := envelope["Failure"].(map[string]any)
					if failure["Code"] != tc.code || failure["Kind"] != "usage" || failure["Hint"] == "" || len(result.stderr) != 0 {
						t.Fatalf("invalid error contract: %s %s", result.stdout, result.stderr)
					}
				} else if len(result.stdout) != 0 || len(result.stderr) == 0 {
					t.Fatalf("text diagnostics in wrong channel: %s %s", result.stdout, result.stderr)
				}
			})
		}
	}
	// A list is not a streaming command. Preserve the shared NDJSON rejection.
	result := eventCenterCLI(t, server, "", "event-center", "list", "-o", "ndjson")
	if result.code != 2 || len(result.stdout) != 0 || !bytes.Contains(result.stderr, []byte("NDJSON_REQUIRES_STREAM")) {
		t.Fatalf("NDJSON rejection: code=%d %s %s", result.code, result.stdout, result.stderr)
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid local input made %d service calls", calls.Load())
	}
}

func TestEventCenterServiceErrorsAndMalformedResponses(t *testing.T) {
	for _, tc := range []struct {
		name, response, code, kind string
		exit                       int
	}{
		{"retention", `{"Response":{"Error":{"Code":"InvalidParameterValue","Message":"outside the retained query window"},"RequestId":"request-error"}}`, "InvalidParameterValue", "usage", 2},
		{"permission", `{"Response":{"Error":{"Code":"UnauthorizedOperation","Message":"denied"},"RequestId":"request-error"}}`, "UnauthorizedOperation", "auth", 4},
		{"service", `{"Response":{"Error":{"Code":"InternalError","Message":"unavailable"},"RequestId":"request-error"}}`, "InternalError", "error", 1},
		{"missing response", `{}`, "INTERNAL_ERROR", "error", 1},
		{"missing list", `{"Response":{"TotalCount":0,"RequestId":"request-error"}}`, "INTERNAL_ERROR", "error", 1},
	} {
		for _, format := range []string{"json", "text"} {
			t.Run(tc.name+"/"+format, func(t *testing.T) {
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, tc.response) }))
				defer server.Close()
				result := eventCenterCLI(t, server, "", "event-center", "list", "--start-time", "2000-01-01T00:00:00Z", "-o", format)
				if result.code != tc.exit {
					t.Fatalf("exit=%d want=%d: %s %s", result.code, tc.exit, result.stdout, result.stderr)
				}
				if format == "text" {
					if len(result.stdout) != 0 || len(result.stderr) == 0 {
						t.Fatalf("wrong diagnostic channel: %s %s", result.stdout, result.stderr)
					}
					return
				}
				var envelope output.Envelope
				if err := json.Unmarshal(result.stdout, &envelope); err != nil {
					t.Fatal(err)
				}
				if envelope.Failure == nil || envelope.Failure.Code != tc.code || envelope.Failure.Kind != tc.kind || len(result.stderr) != 0 {
					t.Fatalf("failure contract: %s %s", result.stdout, result.stderr)
				}
				if strings.Contains(tc.response, `"Error"`) && (envelope.Failure.Details["RequestId"] != "request-error" || envelope.Failure.Hint == "") {
					t.Fatalf("lost service diagnostics: %+v", envelope.Failure)
				}
			})
		}
	}
}

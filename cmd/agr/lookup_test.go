package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
)

func TestInstanceLookupSignedRequests(t *testing.T) {
	requestJSON := `{"InstanceIds":["ins-b","ins-a","ins-b"]}`
	path := filepath.Join(t.TempDir(), "lookup.json")
	if err := os.WriteFile(path, []byte(requestJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, stdin string
		args        []string
	}{
		{"positionals", "", []string{"ins-b", "ins-a", "ins-b"}},
		{"inline", "", []string{"--request", requestJSON}},
		{"file", "", []string{"--request", "@" + path}},
		{"stdin", requestJSON, []string{"--request", "-"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("X-TC-Action") != "DescribeSandboxInstances" || r.Header.Get("Authorization") == "" || r.Header.Get("X-TC-Version") != "2025-09-20" {
					t.Errorf("unsigned request or wrong API identity: %v", r.Header)
				}
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Error(err)
				}
				if !reflect.DeepEqual(got, map[string]any{"InstanceIds": []any{"ins-b", "ins-a", "ins-b"}}) {
					t.Errorf("request changed: %#v", got)
				}
				_, _ = w.Write([]byte(`{"Response":{"InstanceSet":[{"InstanceId":"ins-b","Status":"PAUSED","FutureField":null},{"InstanceId":"ins-a","Status":"RUNNING","LargeInteger":9007199254740993}],"RequestId":"lookup-request"}}`))
			}))
			defer server.Close()
			args := append([]string{"instance", "lookup"}, tc.args...)
			out, stderr, err := runSnapshotCLI(t, server, tc.stdin, args...)
			if err != nil {
				t.Fatalf("lookup: %v; stdout=%s stderr=%s", err, out, stderr)
			}
			var envelope struct {
				Status string
				Meta   struct{ Extra struct{ Action string } }
				Data   struct {
					InstanceSet []map[string]json.RawMessage
					RequestId   string
				}
			}
			if err := json.Unmarshal(out, &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Status != "succeeded" || envelope.Meta.Extra.Action != "DescribeSandboxInstances" || calls.Load() != 1 || len(envelope.Data.InstanceSet) != 2 || envelope.Data.RequestId != "lookup-request" {
				t.Fatalf("invalid lookup result: %s stderr=%s calls=%d", out, stderr, calls.Load())
			}
			first, second := envelope.Data.InstanceSet[0], envelope.Data.InstanceSet[1]
			if string(first["InstanceId"]) != `"ins-b"` || string(first["FutureField"]) != "null" || string(second["LargeInteger"]) != "9007199254740993" {
				t.Fatalf("response order, null, or integer precision lost: %s", out)
			}
		})
	}
}

func TestInstanceLookupInvalidInputsStayLocal(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"Response":{"InstanceSet":[]}}`))
	}))
	defer server.Close()
	tooMany := slices.Repeat([]string{"ins-a"}, 101)
	tooManyJSON, err := json.Marshal(map[string]any{"InstanceIds": tooMany})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, stdin string
		args        []string
	}{
		{"missing IDs", "", nil},
		{"empty ID", "", []string{""}},
		{"whitespace ID", "", []string{" ins-a"}},
		{"long ID", "", []string{strings.Repeat("a", 65)}},
		{"too many positionals", "", tooMany},
		{"too many JSON IDs", "", []string{"--request", string(tooManyJSON)}},
		{"empty object", "", []string{"--request", `{}`}},
		{"empty array", "", []string{"--request", `{"InstanceIds":[]}`}},
		{"null IDs", "", []string{"--request", `{"InstanceIds":null}`}},
		{"scalar IDs", "", []string{"--request", `{"InstanceIds":"ins-a"}`}},
		{"non-string ID", "", []string{"--request", `{"InstanceIds":[1]}`}},
		{"null ID", "", []string{"--request", `{"InstanceIds":[null]}`}},
		{"unknown field", "", []string{"--request", `{"InstanceIds":["ins-a"],"Limit":1}`}},
		{"request conflict", "", []string{"ins-a", "--request", `{"InstanceIds":["ins-a"]}`}},
		{"invalid JSON", "", []string{"--request", `{`}},
		{"missing file", "", []string{"--request", "@/no/such/lookup.json"}},
		{"closed stdin", "", []string{"--request", "-"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"instance", "lookup"}, tc.args...)
			out, stderr, err := runSnapshotCLI(t, server, tc.stdin, args...)
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 2 {
				t.Fatalf("exit=%v stdout=%s stderr=%s", err, out, stderr)
			}
			var envelope struct {
				Status  string
				Failure struct{ Code, Kind string }
			}
			if err := json.Unmarshal(out, &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Status != "failed" || envelope.Failure.Kind != "usage" || envelope.Failure.Code == "" {
				t.Fatalf("invalid usage result: %s", out)
			}
			requireFailureCode(t, schemaForCommand(t, "instance.lookup"), envelope.Failure.Code)
			if calls.Load() != 0 {
				t.Fatal("invalid lookup reached transport")
			}
		})
	}
}

func TestInstanceLookupSchemaAndHelp(t *testing.T) {
	out, err := runAGR(t, "schema", "instance.lookup", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct{ Data cli.CommandSchema }
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	schema := envelope.Data
	if !schema.SupportsRequest || !schema.RequiresAuth || schema.Mutation || schema.Interactive || len(schema.Args) != 1 || !schema.Args[0].Variadic || !schema.Args[0].Required {
		t.Fatalf("lookup schema lost its input contract: %+v", schema)
	}
	property := schema.RequestSchema.Properties["InstanceIds"]
	if property.Type != "array" || !slices.Contains(schema.RequestSchema.Required, "InstanceIds") {
		t.Fatalf("lookup request schema lost the ID array: %+v", schema.RequestSchema)
	}
	for _, flag := range schema.Flags {
		if slices.Contains([]string{"instance-ids", "limit", "offset", "all", "wait"}, flag.Name) {
			t.Errorf("unexpected lookup flag: %s", flag.Name)
		}
	}
	out, err = runAGR(t, "instance", "lookup", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"lookup <instance-id> [instance-id...]", "1 to 100", "persisted", "Duplicate", "omitted", "DescribeSandboxInstances", "--request"} {
		if !strings.Contains(out, text) {
			t.Errorf("lookup help omits %q: %s", text, out)
		}
	}
}

func TestInstanceLookupKeepsGetAndListRoutes(t *testing.T) {
	for _, args := range [][]string{{"get", "ins-a"}, {"list", "--instance-ids", "ins-a"}} {
		t.Run(args[0], func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("X-TC-Action") != "DescribeSandboxInstanceList" {
					t.Errorf("existing command changed action: %s", r.Header.Get("X-TC-Action"))
				}
				_, _ = w.Write([]byte(`{"Response":{"InstanceSet":[{"InstanceId":"ins-a","Status":"UNHEALTHY"}],"TotalCount":1}}`))
			}))
			defer server.Close()
			out, stderr, err := runSnapshotCLI(t, server, "", append([]string{"instance"}, args...)...)
			if err != nil || calls.Load() != 1 || !strings.Contains(string(out), `"UNHEALTHY"`) {
				t.Fatalf("existing command behavior changed: %v stdout=%s stderr=%s calls=%d", err, out, stderr, calls.Load())
			}
		})
	}
}

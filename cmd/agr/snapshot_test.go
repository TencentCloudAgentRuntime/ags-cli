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
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Run the production entry point with fake credentials and a trusted local TLS
// server. Closed stdin and a deadline also catch unexpected interactive prompts.
func runSnapshotCLI(t *testing.T, server *httptest.Server, stdin string, args ...string) ([]byte, string, error) {
	t.Helper()
	return runSnapshotCLIWithStdin(t, server, strings.NewReader(stdin), args...)
}

func runSnapshotCLIWithStdin(t *testing.T, server *httptest.Server, stdin io.Reader, args ...string) ([]byte, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	args = append(args, "-o", "json")
	cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestTransportLifecycleHelper$", "--"}, args...)...)
	home := t.TempDir()
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	cmd.Env = []string{
		"HOME=" + home, "USERPROFILE=" + home, "PATH=" + os.Getenv("PATH"),
		"AGR_TRANSPORT_LIFECYCLE_HELPER=success", "GORACE=atexit_sleep_ms=0",
		"GOCOVERDIR=" + t.TempDir(),
		"TENCENTCLOUD_SECRET_ID=fake", "TENCENTCLOUD_SECRET_KEY=fake", "AGR_REGION=ap-guangzhou",
		"AGR_CLOUD_ENDPOINT=" + strings.TrimPrefix(server.URL, "https://"), "AGR_TRANSPORT_TEST_CERT=" + string(cert),
	}
	cmd.Stdin = stdin
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		t.Fatalf("CLI did not exit: %v; stdout=%s stderr=%s", ctx.Err(), out, stderr.String())
	}
	return out, stderr.String(), err
}

func TestSnapshotAndCloneSignedRequests(t *testing.T) {
	snapshot := map[string]any{
		"SnapshotId": "snapshot-test", "SnapshotName": "fixture", "SourceInstanceId": "ssi-source",
		"MemoryIncluded": false, "Status": "ACTIVE", "Tags": []any{map[string]any{"Key": "purpose", "Value": "test"}},
	}
	createResponse := map[string]any{"Snapshot": snapshot, "RequestId": "request-test"}
	listResponse := map[string]any{"SnapshotSet": []any{snapshot}, "TotalCount": float64(2), "NextToken": "next-page", "RequestId": "request-test"}
	cloneResponse := map[string]any{"Instance": map[string]any{"InstanceId": "ssi-clone", "Status": "STARTING", "AuthMode": "NONE"}, "RequestId": "request-test"}
	for _, tc := range []struct {
		name, action, request, stdin string
		args                         []string
		response                     map[string]any
	}{
		{
			name: "snapshot create flags", action: "CreateSandboxSnapshot",
			args:    []string{"instance", "snapshot", "create", "--instance-id", "ssi-source", "--snapshot-name", "fixture", "--memory=false", "--tags", `[{"Key":"purpose","Value":"test"}]`, "--client-token", "retry-token"},
			request: `{"InstanceId":"ssi-source","SnapshotName":"fixture","Memory":false,"Tags":[{"Key":"purpose","Value":"test"}],"ClientToken":"retry-token"}`, response: createResponse,
		},
		{
			name: "snapshot create stdin", action: "CreateSandboxSnapshot",
			args:    []string{"instance", "snapshot", "create", "--request", "-"},
			stdin:   `{"InstanceId":"ssi-source","SnapshotName":"fixture","Memory":false,"ClientToken":"retry-token"}`,
			request: `{"InstanceId":"ssi-source","SnapshotName":"fixture","Memory":false,"ClientToken":"retry-token"}`, response: createResponse,
		},
		{
			name: "snapshot memory omitted", action: "CreateSandboxSnapshot",
			args:    []string{"instance", "snapshot", "create", "--instance-id", "ssi-source", "--snapshot-name", "fixture"},
			request: `{"InstanceId":"ssi-source","SnapshotName":"fixture"}`, response: createResponse,
		},
		{
			name: "snapshot offset page", action: "DescribeSandboxSnapshotList",
			args:    []string{"instance", "snapshot", "list", "--snapshot-ids", "snapshot-test", "--snapshot-ids", "snapshot-other", "--filters", `[{"Name":"snapshot-name","Values":["fixture"]}]`, "--offset", "0", "--limit", "10"},
			request: `{"SnapshotIds":["snapshot-test","snapshot-other"],"Filters":[{"Name":"snapshot-name","Values":["fixture"]}],"Offset":0,"Limit":10}`, response: listResponse,
		},
		{
			name: "snapshot token page", action: "DescribeSandboxSnapshotList",
			args:    []string{"instance", "snapshot", "list", "--next-token", "previous-page", "--max-results", "10"},
			request: `{"NextToken":"previous-page","MaxResults":10}`, response: listResponse,
		},
		{
			name: "snapshot maximum offset page size", action: "DescribeSandboxSnapshotList",
			args:    []string{"instance", "snapshot", "list", "--limit", "100"},
			request: `{"Limit":100}`, response: listResponse,
		},
		{
			name: "snapshot maximum cursor page size JSON", action: "DescribeSandboxSnapshotList",
			args:    []string{"instance", "snapshot", "list", "--request", `{"MaxResults":100}`},
			request: `{"MaxResults":100}`, response: listResponse,
		},
		{
			name: "snapshot zero pagination values JSON", action: "DescribeSandboxSnapshotList",
			args:    []string{"instance", "snapshot", "list", "--request", `{"Offset":0,"Limit":0,"MaxResults":0}`},
			request: `{"Offset":0,"Limit":0,"MaxResults":0}`, response: listResponse,
		},
		{
			name: "snapshot token with zero offset and limit", action: "DescribeSandboxSnapshotList",
			args:    []string{"instance", "snapshot", "list", "--request", `{"Offset":0,"NextToken":"previous-page","Limit":10}`},
			request: `{"Offset":0,"NextToken":"previous-page","Limit":10}`, response: listResponse,
		},
		{
			name: "snapshot empty token with offset", action: "DescribeSandboxSnapshotList",
			args:    []string{"instance", "snapshot", "list", "--offset", "10", "--next-token", ""},
			request: `{"Offset":10,"NextToken":""}`, response: listResponse,
		},
		{
			name: "snapshot delete positional", action: "DeleteSandboxSnapshot",
			args: []string{"instance", "snapshot", "delete", "snapshot-test"}, request: `{"SnapshotId":"snapshot-test"}`,
			response: map[string]any{"RequestId": "request-test"},
		},
		{
			name: "snapshot delete JSON", action: "DeleteSandboxSnapshot",
			args: []string{"instance", "snapshot", "delete", "--request", `{"SnapshotId":"snapshot-test"}`}, request: `{"SnapshotId":"snapshot-test"}`,
			response: map[string]any{"RequestId": "request-test"},
		},
		{
			name: "snapshot delete yes", action: "DeleteSandboxSnapshot",
			args: []string{"instance", "snapshot", "delete", "snapshot-test", "--yes"}, request: `{"SnapshotId":"snapshot-test"}`,
			response: map[string]any{"RequestId": "request-test"},
		},
		{
			name: "snapshot delete yes shorthand", action: "DeleteSandboxSnapshot",
			args: []string{"instance", "snapshot", "delete", "snapshot-test", "-y"}, request: `{"SnapshotId":"snapshot-test"}`,
			response: map[string]any{"RequestId": "request-test"},
		},
		{
			name: "snapshot delete JSON with yes", action: "DeleteSandboxSnapshot",
			args: []string{"instance", "snapshot", "delete", "--request", `{"SnapshotId":"snapshot-test"}`, "--yes"}, request: `{"SnapshotId":"snapshot-test"}`,
			response: map[string]any{"RequestId": "request-test"},
		},
		{
			name: "snapshot delete stdin with yes", action: "DeleteSandboxSnapshot",
			args: []string{"instance", "snapshot", "delete", "--request", "-", "--yes"}, stdin: `{"SnapshotId":"snapshot-test"}`, request: `{"SnapshotId":"snapshot-test"}`,
			response: map[string]any{"RequestId": "request-test"},
		},
		{
			name: "snapshot delete non-interactive", action: "DeleteSandboxSnapshot",
			args: []string{"instance", "snapshot", "delete", "snapshot-test", "--non-interactive"}, request: `{"SnapshotId":"snapshot-test"}`,
			response: map[string]any{"RequestId": "request-test"},
		},
		{
			name: "clone source flag", action: "StartSandboxInstance",
			args: []string{"instance", "create", "--source-instance-id", "ssi-source"}, request: `{"SourceInstanceId":"ssi-source"}`, response: cloneResponse,
		},
		{
			name: "clone source JSON", action: "StartSandboxInstance",
			args: []string{"instance", "create", "--request", `{"SourceInstanceId":"ssi-source"}`}, request: `{"SourceInstanceId":"ssi-source"}`, response: cloneResponse,
		},
		{
			name: "clone with matching tool", action: "StartSandboxInstance",
			args: []string{"instance", "create", "--source-instance-id", "ssi-source", "--tool-id", "sdt-source"}, request: `{"SourceInstanceId":"ssi-source","ToolId":"sdt-source"}`, response: cloneResponse,
		},
		{
			name: "restore snapshot flag", action: "StartSandboxInstance",
			args: []string{"instance", "create", "--snapshot-id", "snapshot-test"}, request: `{"SnapshotId":"snapshot-test"}`, response: cloneResponse,
		},
		{
			name: "restore snapshot JSON", action: "StartSandboxInstance",
			args: []string{"instance", "create", "--request", `{"SnapshotId":"snapshot-test"}`}, request: `{"SnapshotId":"snapshot-test"}`, response: cloneResponse,
		},
		{
			name: "restore snapshot stdin", action: "StartSandboxInstance",
			args: []string{"instance", "create", "--request", "-"}, stdin: `{"SnapshotId":"snapshot-test"}`, request: `{"SnapshotId":"snapshot-test"}`, response: cloneResponse,
		},
		{
			name: "restore snapshot overrides", action: "StartSandboxInstance",
			args:    []string{"instance", "create", "--snapshot-id", "snapshot-test", "--timeout", "10m", "--client-token", "restore-retry", "--metadata", `[{"Name":"env","Value":"restored"}]`},
			request: `{"SnapshotId":"snapshot-test","Timeout":"10m","ClientToken":"restore-retry","Metadata":[{"Name":"env","Value":"restored"}]}`, response: cloneResponse,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan map[string]any, 8)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-TC-Action") != tc.action || r.Header.Get("Authorization") == "" {
					t.Errorf("action=%s signed=%v", r.Header.Get("X-TC-Action"), r.Header.Get("Authorization") != "")
				}
				var request map[string]any
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				requests <- request
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"Response": tc.response})
			}))
			defer server.Close()
			out, stderr, err := runSnapshotCLI(t, server, tc.stdin, tc.args...)
			if err != nil {
				t.Fatalf("CLI: %v; stdout=%s stderr=%s", err, out, stderr)
			}
			var envelope struct {
				Status string
				Data   map[string]any
			}
			if err := json.Unmarshal(out, &envelope); err != nil {
				t.Fatalf("JSON output: %v; stdout=%s stderr=%s", err, out, stderr)
			}
			if envelope.Status != "succeeded" {
				t.Fatalf("status=%s", envelope.Status)
			}
			for key, want := range tc.response {
				if !reflect.DeepEqual(envelope.Data[key], want) {
					t.Errorf("response %s=%#v, want %#v", key, envelope.Data[key], want)
				}
			}
			var want map[string]any
			if err := json.Unmarshal([]byte(tc.request), &want); err != nil {
				t.Fatal(err)
			}
			if len(requests) != 1 {
				t.Fatalf("API request count=%d, want 1", len(requests))
			}
			if got := <-requests; !reflect.DeepEqual(got, want) {
				t.Errorf("request=%#v, want %#v", got, want)
			}
		})
	}
}

func TestSnapshotDeleteDoesNotReadNonTTYStdin(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("X-TC-Action") != "DeleteSandboxSnapshot" || r.Header.Get("Authorization") == "" {
			t.Errorf("action=%s signed=%v", r.Header.Get("X-TC-Action"), r.Header.Get("Authorization") != "")
		}
		_, _ = w.Write([]byte(`{"Response":{"RequestId":"request-test"}}`))
	}))
	defer server.Close()
	stdin, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdin.Close() }()
	defer func() { _ = writer.Close() }()
	// Keep the pipe open without writing to it: reading a confirmation would
	// block until the CLI helper's deadline instead of seeing EOF.
	out, stderr, err := runSnapshotCLIWithStdin(t, server, stdin, "instance", "snapshot", "delete", "snapshot-test")
	if err != nil {
		t.Fatalf("CLI: %v; stdout=%s stderr=%s", err, out, stderr)
	}
	var envelope struct {
		Status string
		Data   struct{ RequestId string }
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		t.Fatalf("JSON output: %v; stdout=%s stderr=%s", err, out, stderr)
	}
	if envelope.Status != "succeeded" || envelope.Data.RequestId != "request-test" || calls.Load() != 1 {
		t.Fatalf("unexpected delete result: calls=%d stdout=%s stderr=%s", calls.Load(), out, stderr)
	}
	if strings.Contains(stderr, "Proceed?") {
		t.Fatalf("non-TTY delete prompted for confirmation: %s", stderr)
	}
}

func TestSnapshotAndInstanceDeleteConfirmationHelpAndSchema(t *testing.T) {
	root := contractRoot()
	for _, id := range []string{"instance.snapshot.delete", "instance.delete"} {
		t.Run(id, func(t *testing.T) {
			cmd, ok := findCobraCommand(root, id)
			if !ok {
				t.Fatalf("command %s not found", id)
			}
			yes := cmd.Flags().Lookup("yes")
			if yes == nil || yes.Value.Type() != "bool" || yes.Shorthand != "y" || yes.DefValue != "false" {
				t.Fatalf("runtime --yes flag=%+v", yes)
			}
			helpArgs := append(strings.Split(id, "."), "--help")
			help, err := runAGR(t, helpArgs...)
			if err != nil {
				t.Fatalf("help failed: %v; output=%s", err, help)
			}
			for _, text := range []string{"TTY", "--yes", "-y", "--non-interactive", yes.Usage} {
				if !strings.Contains(help, text) {
					t.Errorf("help is missing %q: %s", text, help)
				}
			}
			if strings.Contains(help, "does not prompt for confirmation") || strings.Contains(help, "no confirmation prompt") {
				t.Errorf("help retains unconditional no-confirmation claim: %s", help)
			}
			out, err := runAGR(t, "schema", id, "-o", "json")
			if err != nil {
				t.Fatalf("schema failed: %v; output=%s", err, out)
			}
			var envelope struct {
				Data struct {
					Flags         []struct{ Name, Shorthand, Type, Description, Default string }
					RequestSchema struct {
						Properties map[string]json.RawMessage
					}
				}
			}
			if err := json.Unmarshal([]byte(out), &envelope); err != nil {
				t.Fatal(err)
			}
			flagIndex := slices.IndexFunc(envelope.Data.Flags, func(flag struct{ Name, Shorthand, Type, Description, Default string }) bool {
				return flag.Name == "yes"
			})
			if flagIndex < 0 {
				t.Fatal("schema is missing --yes")
			}
			flag := envelope.Data.Flags[flagIndex]
			if flag.Type != "bool" || flag.Shorthand != "y" || flag.Description != yes.Usage || flag.Default != yes.DefValue {
				t.Errorf("schema --yes flag=%+v differs from runtime flag=%+v", flag, yes)
			}
			for property := range envelope.Data.RequestSchema.Properties {
				if strings.EqualFold(property, "yes") {
					t.Errorf("workflow --yes leaked into request schema as %q", property)
				}
			}
		})
	}
}

func TestSnapshotAndCloneInvalidInputsStayLocal(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"Response":{"RequestId":"unexpected"}}`))
	}))
	defer server.Close()
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"delete missing ID", []string{"instance", "snapshot", "delete"}},
		{"delete empty request", []string{"instance", "snapshot", "delete", "--request", `{}`}},
		{"offset with token", []string{"instance", "snapshot", "list", "--offset", "1", "--next-token", "next"}},
		{"offset with token JSON", []string{"instance", "snapshot", "list", "--request", `{"Offset":1,"NextToken":"next"}`}},
		{"max results with offset", []string{"instance", "snapshot", "list", "--max-results", "10", "--offset", "1"}},
		{"max results with limit JSON", []string{"instance", "snapshot", "list", "--request", `{"MaxResults":10,"Limit":10}`}},
		{"negative offset", []string{"instance", "snapshot", "list", "--offset", "-1"}},
		{"negative offset JSON", []string{"instance", "snapshot", "list", "--request", `{"Offset":-1}`}},
		{"negative limit", []string{"instance", "snapshot", "list", "--limit", "-1"}},
		{"negative limit JSON", []string{"instance", "snapshot", "list", "--request", `{"Limit":-1}`}},
		{"negative max results", []string{"instance", "snapshot", "list", "--max-results", "-1"}},
		{"negative max results JSON", []string{"instance", "snapshot", "list", "--request", `{"MaxResults":-1}`}},
		{"limit too large", []string{"instance", "snapshot", "list", "--limit", "101"}},
		{"limit too large JSON", []string{"instance", "snapshot", "list", "--request", `{"Limit":101}`}},
		{"max results too large", []string{"instance", "snapshot", "list", "--max-results", "101"}},
		{"max results too large JSON", []string{"instance", "snapshot", "list", "--request", `{"MaxResults":101}`}},
		{"offset overflow JSON", []string{"instance", "snapshot", "list", "--request", `{"Offset":9223372036854775808}`}},
		{"clone missing source", []string{"instance", "create"}},
		{"clone conflicting tools", []string{"instance", "create", "--source-instance-id", "ssi-source", "--tool-id", "sdt-source", "--tool-name", "source"}},
		{"clone request conflicts with flag", []string{"instance", "create", "--source-instance-id", "ssi-source", "--request", `{"SourceInstanceId":"ssi-source"}`}},
		{"restore request conflicts with flag", []string{"instance", "create", "--snapshot-id", "snapshot-test", "--request", `{"SnapshotId":"snapshot-test"}`}},
		{"restore conflicts with clone", []string{"instance", "create", "--snapshot-id", "snapshot-test", "--source-instance-id", "ssi-source"}},
		{"restore conflicts with clone JSON", []string{"instance", "create", "--request", `{"SnapshotId":"snapshot-test","SourceInstanceId":"ssi-source"}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := calls.Load()
			out, stderr, err := runSnapshotCLI(t, server, "", tc.args...)
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
				t.Fatalf("invalid usage failure: %s", out)
			}
			pathLength := 2
			if tc.args[1] == "snapshot" {
				pathLength = 3
			}
			schema := schemaForCommand(t, strings.Join(tc.args[:pathLength], "."))
			requireFailureCode(t, schema, envelope.Failure.Code)
			if calls.Load() != before {
				t.Fatal("invalid input sent an API request")
			}
		})
	}
}

func TestSnapshotNamespaceAndCloneSchema(t *testing.T) {
	root := contractRoot()
	group, ok := findCobraCommand(root, "instance.snapshot")
	if !ok {
		t.Fatal("snapshot group is absent")
	}
	if help := commandHelp(t, group); !strings.Contains(help, "Examples:") {
		t.Fatalf("snapshot group has no workflow example: %s", help)
	}
	for _, id := range []string{"instance.snapshot.create", "instance.snapshot.list", "instance.snapshot.delete"} {
		schema := schemaForCommand(t, id)
		if schema.Kind != "command" || !schema.RequiresAuth || !schema.SupportsRequest {
			t.Errorf("%s lost command/auth/request metadata: %+v", id, schema)
		}
	}
	schema := schemaForCommand(t, "instance.create")
	create, _ := findCobraCommand(root, "instance.create")
	for field, flag := range map[string]string{"SourceInstanceId": "source-instance-id", "SnapshotId": "snapshot-id"} {
		property, ok := schema.RequestSchema[field]
		if !ok || property.Type != "string" || property.CliFlag == nil || *property.CliFlag != flag {
			t.Fatalf("%s property=%+v present=%v", field, property, ok)
		}
		if create.Flags().Lookup(flag) == nil || !strings.Contains(commandHelp(t, create), "--"+flag) {
			t.Fatalf("%s is absent from runtime/help", flag)
		}
	}
	out, err := runAGR(t, "schema", "instance.create", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			Flags []struct {
				Name             string
				IncompatibleWith []string
			}
			RequestSchema struct {
				AnyOf []struct{ Required []string }
			}
		}
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"SourceInstanceId", "SnapshotId"} {
		if !slices.ContainsFunc(envelope.Data.RequestSchema.AnyOf, func(requirement struct{ Required []string }) bool {
			return slices.Equal(requirement.Required, []string{field})
		}) {
			t.Fatalf("schema does not allow %s as an independent create source", field)
		}
	}
	for flag, incompatible := range map[string]string{"source-instance-id": "snapshot-id", "snapshot-id": "source-instance-id"} {
		if !slices.ContainsFunc(envelope.Data.Flags, func(entry struct {
			Name             string
			IncompatibleWith []string
		}) bool {
			return entry.Name == flag && slices.Contains(entry.IncompatibleWith, incompatible)
		}) {
			t.Fatalf("schema does not declare %s incompatible with %s", flag, incompatible)
		}
	}
}

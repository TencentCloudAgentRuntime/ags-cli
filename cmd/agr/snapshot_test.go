package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
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
	cmd.Stdin = strings.NewReader(stdin)
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
		{"clone missing source", []string{"instance", "create"}},
		{"clone conflicting tools", []string{"instance", "create", "--source-instance-id", "ssi-source", "--tool-id", "sdt-source", "--tool-name", "source"}},
		{"clone request conflicts with flag", []string{"instance", "create", "--source-instance-id", "ssi-source", "--request", `{"SourceInstanceId":"ssi-source"}`}},
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
	property, ok := schema.RequestSchema["SourceInstanceId"]
	if !ok || property.Type != "string" || property.CliFlag == nil || *property.CliFlag != "source-instance-id" {
		t.Fatalf("SourceInstanceId property=%+v present=%v", property, ok)
	}
	create, _ := findCobraCommand(root, "instance.create")
	if create.Flags().Lookup("source-instance-id") == nil || !strings.Contains(commandHelp(t, create), "--source-instance-id") {
		t.Fatal("clone source flag is absent from runtime/help")
	}
	out, err := runAGR(t, "schema", "instance.create", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			RequestSchema struct {
				AnyOf []struct{ Required []string }
			}
		}
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(envelope.Data.RequestSchema.AnyOf, func(requirement struct{ Required []string }) bool {
		return slices.Equal(requirement.Required, []string{"SourceInstanceId"})
	}) {
		t.Fatal("schema does not allow SourceInstanceId as an independent create source")
	}
}

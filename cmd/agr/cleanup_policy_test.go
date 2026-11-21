package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

type cleanupPolicyRequest struct {
	action  string
	payload map[string]any
}

// cleanupPolicyServer records the signed requests reaching the transport and
// replies with the supplied response body.
func cleanupPolicyServer(t *testing.T, body func(action string, payload map[string]any) string) (*httptest.Server, chan cleanupPolicyRequest) {
	t.Helper()
	requests := make(chan cleanupPolicyRequest, 8)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" || r.Header.Get("X-TC-Version") != "2025-09-20" {
			t.Error("missing signature or wrong API version")
		}
		action := r.Header.Get("X-TC-Action")
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		requests <- cleanupPolicyRequest{action: action, payload: payload}
		_, _ = w.Write([]byte(body(action, payload)))
	}))
	t.Cleanup(server.Close)
	return server, requests
}

func cleanupPolicyCreateResponse(id string) string {
	return `{"Response":{"CleanupPolicy":{"CleanupPolicyId":"` + id + `","Name":"demo","Enabled":true,"IdleTTL":"3d","RegistryHost":"example.invalid","RepoPattern":"^agr-issue180/run$"},"RequestId":"request-create"}}`
}

func TestCleanupPolicySignedTransport(t *testing.T) {
	const filter = `[{"Name":"name","Values":["demo"]}]`
	filterPath := filepath.Join(t.TempDir(), "filter.json")
	if err := os.WriteFile(filterPath, []byte(filter), 0o600); err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.Join(t.TempDir(), "create.json")
	if err := os.WriteFile(requestPath, []byte(`{"Name":"demo","IdleTTL":"3d"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		args       []string
		stdin      string
		wantAction string
		want       map[string]any
	}{
		{
			name:       "create flags",
			args:       []string{"create", "--name", "demo", "--idle-ttl", "3d", "--registry-host", "example.invalid", "--repo-pattern", "^agr-issue180/run$", "--enabled=false"},
			wantAction: "CreatePreCacheImageCleanupPolicy",
			want:       map[string]any{"Name": "demo", "IdleTTL": "3d", "RegistryHost": "example.invalid", "RepoPattern": "^agr-issue180/run$", "Enabled": false},
		},
		{
			name:       "create omits enabled",
			args:       []string{"create", "--name", "demo", "--idle-ttl", "3d"},
			wantAction: "CreatePreCacheImageCleanupPolicy",
			want:       map[string]any{"Name": "demo", "IdleTTL": "3d"},
		},
		{
			name:       "create request",
			args:       []string{"create", "--request", `{"Name":"demo","IdleTTL":"12h"}`},
			wantAction: "CreatePreCacheImageCleanupPolicy",
			want:       map[string]any{"Name": "demo", "IdleTTL": "12h"},
		},
		{
			name:       "create request file",
			args:       []string{"create", "--request", "@" + requestPath},
			wantAction: "CreatePreCacheImageCleanupPolicy",
			want:       map[string]any{"Name": "demo", "IdleTTL": "3d"},
		},
		{
			name:       "create request stdin",
			args:       []string{"create", "--request", "-"},
			stdin:      `{"Name":"demo","IdleTTL":"3d"}`,
			wantAction: "CreatePreCacheImageCleanupPolicy",
			want:       map[string]any{"Name": "demo", "IdleTTL": "3d"},
		},
		{
			name:       "update flags",
			args:       []string{"update", "pccp-a", "--enabled=false", "--repo-pattern="},
			wantAction: "UpdatePreCacheImageCleanupPolicy",
			want:       map[string]any{"CleanupPolicyId": "pccp-a", "Enabled": false, "RepoPattern": ""},
		},
		{
			name:       "update request merge",
			args:       []string{"update", "pccp-a", "--request", `{"CleanupPolicyId":"pccp-a","IdleTTL":"7d"}`},
			wantAction: "UpdatePreCacheImageCleanupPolicy",
			want:       map[string]any{"CleanupPolicyId": "pccp-a", "IdleTTL": "7d"},
		},
		{
			name:       "list repeated ids",
			args:       []string{"list", "--cleanup-policy-ids", "pccp-a", "--cleanup-policy-ids", "pccp-b"},
			wantAction: "DescribePreCacheImageCleanupPolicyList",
			want:       map[string]any{"CleanupPolicyIds": []any{"pccp-a", "pccp-b"}},
		},
		{
			name:       "list ids request",
			args:       []string{"list", "--request", `{"CleanupPolicyIds":["pccp-a","pccp-b"]}`},
			wantAction: "DescribePreCacheImageCleanupPolicyList",
			want:       map[string]any{"CleanupPolicyIds": []any{"pccp-a", "pccp-b"}},
		},
		{
			name:       "list filter inline",
			args:       []string{"list", "--filter", filter, "--offset", "0", "--limit", "1"},
			wantAction: "DescribePreCacheImageCleanupPolicyList",
			want:       map[string]any{"Filters": []any{map[string]any{"Name": "name", "Values": []any{"demo"}}}, "Offset": float64(0), "Limit": float64(1)},
		},
		{
			name:       "list filter alias file",
			args:       []string{"list", "--filters", "@" + filterPath},
			wantAction: "DescribePreCacheImageCleanupPolicyList",
			want:       map[string]any{"Filters": []any{map[string]any{"Name": "name", "Values": []any{"demo"}}}},
		},
		{
			name:       "list filter stdin",
			args:       []string{"list", "--filter", "-"},
			stdin:      filter,
			wantAction: "DescribePreCacheImageCleanupPolicyList",
			want:       map[string]any{"Filters": []any{map[string]any{"Name": "name", "Values": []any{"demo"}}}},
		},
		{
			name:       "update request only",
			args:       []string{"update", "--request", `{"CleanupPolicyId":"pccp-a","Enabled":false}`},
			wantAction: "UpdatePreCacheImageCleanupPolicy",
			want:       map[string]any{"CleanupPolicyId": "pccp-a", "Enabled": false},
		},
		{
			name:       "delete request only",
			args:       []string{"delete", "--request", `{"CleanupPolicyId":"pccp-a"}`},
			wantAction: "DeletePreCacheImageCleanupPolicy",
			want:       map[string]any{"CleanupPolicyId": "pccp-a"},
		},
		{
			name:       "delete positional",
			args:       []string{"delete", "pccp-a"},
			wantAction: "DeletePreCacheImageCleanupPolicy",
			want:       map[string]any{"CleanupPolicyId": "pccp-a"},
		},
		{
			name:       "delete request merge",
			args:       []string{"delete", "pccp-a", "--request", `{"CleanupPolicyId":"pccp-a"}`},
			wantAction: "DeletePreCacheImageCleanupPolicy",
			want:       map[string]any{"CleanupPolicyId": "pccp-a"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, requests := cleanupPolicyServer(t, func(action string, _ map[string]any) string {
				switch action {
				case "CreatePreCacheImageCleanupPolicy", "UpdatePreCacheImageCleanupPolicy":
					return cleanupPolicyCreateResponse("pccp-a")
				case "DescribePreCacheImageCleanupPolicyList":
					return `{"Response":{"CleanupPolicySet":[{"CleanupPolicyId":"pccp-a","Name":"demo","Enabled":true,"IdleTTL":"3d"}],"TotalCount":1,"RequestId":"request-list"}}`
				default:
					return `{"Response":{"RequestId":"request-delete"}}`
				}
			})
			out, stderr, err := runSnapshotCLI(t, server, tc.stdin, append([]string{"pre-cache-image-cleanup-policy"}, tc.args...)...)
			if err != nil {
				t.Fatalf("CLI: %v; stdout=%s stderr=%s", err, out, stderr)
			}
			var envelope struct {
				SchemaVersion string
				Command       string
				Status        string
			}
			if err := json.Unmarshal(out, &envelope); err != nil || envelope.Status != "succeeded" || envelope.Command != "pre-cache-image-cleanup-policy."+tc.args[0] {
				t.Fatalf("invalid success envelope: %s (%v)", out, err)
			}
			select {
			case request := <-requests:
				if request.action != tc.wantAction {
					t.Fatalf("action = %s, want %s", request.action, tc.wantAction)
				}
				if !reflect.DeepEqual(request.payload, tc.want) {
					t.Fatalf("signed request = %#v, want %#v", request.payload, tc.want)
				}
			default:
				t.Fatalf("no %s request", tc.wantAction)
			}
		})
	}
}

func TestCleanupPolicyLocalValidation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "local validation leaked to the transport", http.StatusInternalServerError)
	}))
	defer server.Close()
	for _, tc := range []struct {
		name string
		args []string
		code string
	}{
		{"create missing name", []string{"create", "--idle-ttl", "3d"}, "MISSING_REQUIRED_FLAG"},
		{"create missing ttl", []string{"create", "--name", "demo"}, "MISSING_REQUIRED_FLAG"},
		{"create zero ttl", []string{"create", "--name", "demo", "--idle-ttl", "0"}, "INVALID_IDLE_TTL"},
		{"create fractional ttl", []string{"create", "--name", "demo", "--idle-ttl", "1.5h"}, "INVALID_IDLE_TTL"},
		{"create compound ttl", []string{"create", "--name", "demo", "--idle-ttl", "1h30m"}, "INVALID_IDLE_TTL"},
		{"create seconds ttl", []string{"create", "--name", "demo", "--idle-ttl", "1s"}, "INVALID_IDLE_TTL"},
		{"create request ttl", []string{"create", "--request", `{"Name":"demo","IdleTTL":3}`}, "INVALID_IDLE_TTL"},
		{"update empty patch", []string{"update", "pccp-a"}, "MISSING_REQUIRED_FLAG"},
		{"update null patch", []string{"update", "pccp-a", "--request", `{"CleanupPolicyId":"pccp-a","Name":null,"Enabled":null,"IdleTTL":null,"RegistryHost":null,"RepoPattern":null}`}, "MISSING_REQUIRED_FLAG"},
		{"update mismatched positional", []string{"update", "pccp-a", "--request", `{"CleanupPolicyId":"pccp-b","Enabled":false}`}, "REQUEST_ARG_CONFLICT"},
		{"delete mismatched positional", []string{"delete", "pccp-a", "--request", `{"CleanupPolicyId":"pccp-b"}`}, "REQUEST_ARG_CONFLICT"},
		{"delete empty request", []string{"delete", "--request", `{}`}, "MISSING_REQUIRED_ARG"},
		{"list invalid filter", []string{"list", "--filter", `{"Name":"name"}`}, "INVALID_REQUEST_JSON"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls.Store(0)
			out, stderr, err := runSnapshotCLI(t, server, "", append([]string{"pre-cache-image-cleanup-policy"}, tc.args...)...)
			var envelope struct{ Failure struct{ Code string } }
			if err == nil || json.Unmarshal(out, &envelope) != nil {
				t.Fatalf("expected a usage failure: %s %s (%v)", out, stderr, err)
			}
			if envelope.Failure.Code != tc.code {
				t.Fatalf("failure code = %q, want %q: %s", envelope.Failure.Code, tc.code, out)
			}
			if calls.Load() != 0 {
				t.Fatal("invalid input caused an API call")
			}
		})
	}
}

func TestCleanupPolicyOutputs(t *testing.T) {
	server, _ := cleanupPolicyServer(t, func(action string, _ map[string]any) string {
		switch action {
		case "CreatePreCacheImageCleanupPolicy":
			return cleanupPolicyCreateResponse("pccp-a")
		case "UpdatePreCacheImageCleanupPolicy":
			return `{"Response":{"CleanupPolicy":{"CleanupPolicyId":"pccp-a","Name":"demo","Enabled":false,"IdleTTL":"3d","RegistryHost":"example.invalid","RepoPattern":""},"RequestId":"request-update"}}`
		case "DescribePreCacheImageCleanupPolicyList":
			return `{"Response":{"CleanupPolicySet":[{"CleanupPolicyId":"pccp-a","Name":"demo","Enabled":true,"IdleTTL":"3d"}],"TotalCount":2,"RequestId":"request-list"}}`
		default:
			return `{"Response":{"RequestId":"request-delete"}}`
		}
	})
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{"create", []string{"create", "--name", "demo", "--idle-ttl", "3d"}, []string{"CleanupPolicyId:", "pccp-a", "IdleTTL:", "3d", "RepoPattern:", "^agr-issue180/run$"}},
		{"update", []string{"update", "pccp-a", "--enabled=false"}, []string{"CleanupPolicyId:", "Enabled:", "false", "RepoPattern:"}},
		{"list", []string{"list"}, []string{"ID", "NAME", "ENABLED", "IDLE TTL", "pccp-a", "demo", "Total: 2"}},
		{"delete", []string{"delete", "pccp-a"}, []string{"Pre-cache image cleanup policy deleted: pccp-a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, stderr, err := runSnapshotCLIWithOutput(t, server, strings.NewReader(""), "text", append([]string{"pre-cache-image-cleanup-policy"}, tc.args...)...)
			if err != nil {
				t.Fatalf("CLI: %v; stdout=%s stderr=%s", err, out, stderr)
			}
			for _, fragment := range tc.want {
				if !bytes.Contains(out, []byte(fragment)) {
					t.Errorf("text output missing %q: %s", fragment, out)
				}
			}
		})
	}
}

func TestCleanupPolicyResponseHandling(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		response string
		code     string
		exit     int
		want     []string
	}{
		{
			name:     "create without cleanup policy id",
			args:     []string{"create", "--name", "demo", "--idle-ttl", "3d"},
			response: `{"Response":{"RequestId":"request-create"}}`,
			code:     "INTERNAL_ERROR",
			exit:     1,
		},
		{
			name:     "update without cleanup policy id",
			args:     []string{"update", "pccp-a", "--enabled=false"},
			response: `{"Response":{"CleanupPolicy":{},"RequestId":"request-update"}}`,
			code:     "INTERNAL_ERROR",
			exit:     1,
		},
		{
			name:     "delete without request id",
			args:     []string{"delete", "pccp-a"},
			response: `{"Response":{}}`,
			code:     "INTERNAL_ERROR",
			exit:     1,
		},
		{
			name:     "list malformed set",
			args:     []string{"list"},
			response: `{"Response":{"CleanupPolicySet":[null],"TotalCount":1,"RequestId":"request-list"}}`,
			code:     "INTERNAL_ERROR",
			exit:     1,
		},
		{
			name:     "service error keeps the code",
			args:     []string{"delete", "pccp-a"},
			response: `{"Response":{"Error":{"Code":"InvalidParameterValue","Message":"policy is in use"},"RequestId":"request-error"}}`,
			code:     "InvalidParameterValue",
			exit:     2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := cleanupPolicyServer(t, func(string, map[string]any) string { return tc.response })
			out, stderr, err := runSnapshotCLI(t, server, "", append([]string{"pre-cache-image-cleanup-policy"}, tc.args...)...)
			var envelope struct{ Failure struct{ Code string } }
			if err == nil || json.Unmarshal(out, &envelope) != nil {
				t.Fatalf("expected a failure: %s %s (%v)", out, stderr, err)
			}
			if envelope.Failure.Code != tc.code {
				t.Fatalf("failure code = %q, want %q: %s", envelope.Failure.Code, tc.code, out)
			}
			if exitCode := exitCodeOf(err); exitCode != tc.exit {
				t.Fatalf("exit code = %d, want %d: %s", exitCode, tc.exit, out)
			}
			for _, fragment := range tc.want {
				if !bytes.Contains(out, []byte(fragment)) {
					t.Errorf("failure output missing %q: %s", fragment, out)
				}
			}
		})
	}
	server, _ := cleanupPolicyServer(t, func(string, map[string]any) string {
		return `{"Response":{"CleanupPolicySet":[],"TotalCount":0,"RequestId":"request-list"}}`
	})
	out, stderr, err := runSnapshotCLIWithOutput(t, server, nil, "text", "pre-cache-image-cleanup-policy", "list")
	if err != nil {
		t.Fatalf("CLI: %v; stdout=%s stderr=%s", err, out, stderr)
	}
	if !bytes.Contains(out, []byte("No pre-cache image cleanup policies found")) || !bytes.Contains(out, []byte("Total: 0")) {
		t.Fatalf("empty page output = %s", out)
	}
}

func TestCleanupPolicyHelpAndSchema(t *testing.T) {
	root := contractRoot()
	group, ok := findCobraCommand(root, "pre-cache-image-cleanup-policy")
	if !ok {
		t.Fatal("cleanup policy group missing")
	}
	if rootHelp := commandHelp(t, root); !strings.Contains(rootHelp, "pre-cache-image-cleanup-policy") {
		t.Errorf("root help does not list the cleanup policy group:\n%s", rootHelp)
	}
	groupHelp := commandHelp(t, group)
	for _, fragment := range []string{"Manage account-level policies that clean up pre-cached images", "agr pre-cache-image-cleanup-policy create", "agr pre-cache-image-cleanup-policy delete"} {
		if !strings.Contains(groupHelp, fragment) {
			t.Errorf("group help missing %q:\n%s", fragment, groupHelp)
		}
	}
	for _, tc := range []struct {
		command  string
		wantArgs []string
		checks   map[string]string
	}{
		{
			command:  "pre-cache-image-cleanup-policy.create",
			checks:   map[string]string{"Name": "name", "IdleTTL": "idle-ttl", "RegistryHost": "registry-host", "RepoPattern": "repo-pattern", "Enabled": "enabled"},
			wantArgs: nil,
		},
		{
			command:  "pre-cache-image-cleanup-policy.update",
			checks:   map[string]string{"Name": "name", "IdleTTL": "idle-ttl", "RegistryHost": "registry-host", "RepoPattern": "repo-pattern", "Enabled": "enabled"},
			wantArgs: []string{"CleanupPolicyId"},
		},
		{
			command:  "pre-cache-image-cleanup-policy.list",
			checks:   map[string]string{"CleanupPolicyIds": "cleanup-policy-ids", "Filters": "filter", "Offset": "offset", "Limit": "limit"},
			wantArgs: nil,
		},
		{
			command:  "pre-cache-image-cleanup-policy.delete",
			checks:   map[string]string{},
			wantArgs: []string{"CleanupPolicyId"},
		},
	} {
		t.Run(tc.command, func(t *testing.T) {
			schema := schemaForCommand(t, tc.command)
			if len(tc.wantArgs) > 0 {
				requireArgNames(t, schema, tc.wantArgs...)
			}
			for property, flagName := range tc.checks {
				requireCliFlag(t, schema, property, flagName)
				if _, ok := schema.Flags[flagName]; !ok {
					t.Errorf("schema flags missing %q", flagName)
				}
			}
			if property, ok := tc.checks["CleanupPolicyIds"]; ok {
				requirePropertyType(t, schema, "CleanupPolicyIds", "array")
				_ = property
			}
			if _, ok := tc.checks["Enabled"]; ok {
				requirePropertyType(t, schema, "Enabled", "bool")
			}
			cmd, ok := findCobraCommand(root, tc.command)
			if !ok {
				t.Fatalf("%s command missing", tc.command)
			}
			if help := commandHelp(t, cmd); !strings.Contains(help, "Example - ") || !strings.Contains(help, "agr pre-cache-image-cleanup-policy") {
				t.Errorf("%s lacks executable examples:\n%s", tc.command, help)
			}
		})
	}

	list := schemaForCommand(t, "pre-cache-image-cleanup-policy.list")
	if list.Flags["filter"].Type != "json" || list.Flags["cleanup-policy-ids"].Type != "string_array" {
		t.Fatalf("list flag types drifted: %+v", list.Flags)
	}
	listCmd, _ := findCobraCommand(root, "pre-cache-image-cleanup-policy.list")
	listHelp := commandHelp(t, listCmd)
	for _, fragment := range []string{
		"--cleanup-policy-ids pccp-a --cleanup-policy-ids pccp-b",
		"--request '{\"CleanupPolicyIds\":[\"pccp-a\",\"pccp-b\"]}'",
		"100 IDs",
		"1–100",
		"Offset",
	} {
		if !strings.Contains(listHelp, fragment) {
			t.Errorf("list help missing %q:\n%s", fragment, listHelp)
		}
	}
	createCmd, _ := findCobraCommand(root, "pre-cache-image-cleanup-policy.create")
	createHelp := commandHelp(t, createCmd)
	if !strings.Contains(createHelp, "--enabled=false") {
		t.Errorf("create help lacks the disabled example:\n%s", createHelp)
	}
	// There is no show command for a policy, so the create help must point at
	// the ID lookup that exists.
	if !strings.Contains(createHelp, "list --cleanup-policy-ids") {
		t.Errorf("create help does not name the ID lookup:\n%s", createHelp)
	}
	deleteCmd, _ := findCobraCommand(root, "pre-cache-image-cleanup-policy.delete")
	deleteHelp := commandHelp(t, deleteCmd)
	if !strings.Contains(deleteHelp, "already deleted returns success") {
		t.Errorf("delete help misses the soft-delete outcome:\n%s", deleteHelp)
	}
	if strings.Contains(deleteHelp, "not idempotent at the response level") {
		t.Errorf("delete help still claims a retried delete fails:\n%s", deleteHelp)
	}
}

func exitCodeOf(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 0
}

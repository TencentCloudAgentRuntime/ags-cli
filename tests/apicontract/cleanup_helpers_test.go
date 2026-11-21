package apicontract_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/tests/testutil"
)

// scriptedRunner answers CLI calls from a per-test function so the cleanup
// recovery paths, which only run when a create response is lost or a lookup
// fails, can be exercised without a live endpoint.
type scriptedRunner struct {
	calls   [][]string
	respond func(args []string) testutil.CommandResult
}

func (r *scriptedRunner) Run(_ context.Context, args ...string) testutil.CommandResult {
	r.calls = append(r.calls, append([]string(nil), args...))
	return r.respond(args)
}

func scriptedSucceeded(t *testing.T, command string, data map[string]any) testutil.CommandResult {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"SchemaVersion": "agr.v1",
		"Command":       command,
		"Status":        "succeeded",
		"Data":          data,
	})
	if err != nil {
		t.Fatalf("marshal %s envelope: %v", command, err)
	}
	return testutil.CommandResult{Args: []string{command}, Stdout: string(body)}
}

func scriptedFailed(t *testing.T, command, code, kind string) testutil.CommandResult {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"SchemaVersion": "agr.v1",
		"Command":       command,
		"Status":        "failed",
		"Failure":       map[string]any{"Code": code, "Kind": kind, "Message": code},
	})
	if err != nil {
		t.Fatalf("marshal %s failure: %v", command, err)
	}
	return testutil.CommandResult{Args: []string{command}, Stdout: string(body), ExitCode: 1}
}

// flagValue returns the value that follows an exact flag in a CLI argument list.
func flagValue(t *testing.T, args []string, flag string) string {
	t.Helper()
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	t.Fatalf("argument list %v has no %s", args, flag)
	return ""
}

func TestFindToolByNameUsesTheExactNameFilter(t *testing.T) {
	name := "agr-issue180-1700000000000000000"
	runner := &scriptedRunner{respond: func(args []string) testutil.CommandResult {
		if len(args) < 2 || args[0] != "tool" || args[1] != "list" {
			t.Fatalf("unexpected call %v", args)
		}
		var filters []map[string]any
		if err := json.Unmarshal([]byte(flagValue(t, args, "--filters")), &filters); err != nil {
			t.Fatalf("filters are not JSON: %v", err)
		}
		if len(filters) != 1 {
			t.Fatalf("filters = %#v", filters)
		}
		// The service lowercases the filter name and only its own lowercase
		// keys select the exact-name filter; "ToolName" would be ignored and the
		// list would scan the account's first page instead.
		if key := filters[0]["Name"]; key != "tool-name" {
			t.Fatalf("filter key = %v, want tool-name", key)
		}
		values, _ := filters[0]["Values"].([]any)
		if len(values) != 1 || values[0] != name {
			t.Fatalf("filter values = %#v, want only %q", values, name)
		}
		return scriptedSucceeded(t, "tool.list", map[string]any{"Items": []any{
			map[string]any{"ToolId": "sdt-other", "ToolName": "agr-issue180-1699999999999999999"},
			map[string]any{"ToolId": "sdt-this-run", "ToolName": name},
		}})
	}}
	id, err := findToolByName(context.Background(), runner, name)
	if err != nil {
		t.Fatalf("findToolByName: %v", err)
	}
	if id != "sdt-this-run" {
		t.Fatalf("tool ID = %q, want sdt-this-run", id)
	}
}

func TestFindToolByNameReportsLookupFailure(t *testing.T) {
	runner := &scriptedRunner{respond: func(args []string) testutil.CommandResult {
		return scriptedFailed(t, "tool.list", "InternalError", "server_error")
	}}
	id, err := findToolByName(context.Background(), runner, "agr-issue180-unknown")
	if id != "" {
		t.Fatalf("tool ID = %q, want empty", id)
	}
	if err == nil || !strings.Contains(err.Error(), "look up tool") {
		t.Fatalf("error = %v, want a lookup failure that reports cleanup is unverified", err)
	}
}

func TestFindToolByNameReportsUndecodableList(t *testing.T) {
	runner := &scriptedRunner{respond: func(args []string) testutil.CommandResult {
		return testutil.CommandResult{Args: args, Stdout: "not json"}
	}}
	if _, err := findToolByName(context.Background(), runner, "agr-issue180-unknown"); err == nil {
		t.Fatal("a tool list that is not an envelope must report a lookup failure")
	}
}

func TestCleanupInstancesByToolOnlyTouchesTheRunTool(t *testing.T) {
	const toolID = "sdt-this-run"
	var deleted []string
	runner := &scriptedRunner{respond: func(args []string) testutil.CommandResult {
		switch {
		case args[0] == "instance" && args[1] == "list":
			if got := flagValue(t, args, "--tool-id"); got != toolID {
				t.Fatalf("--tool-id = %q, want %q", got, toolID)
			}
			return scriptedSucceeded(t, "instance.list", map[string]any{"Items": []any{
				map[string]any{"InstanceId": "ins-this-run", "ToolId": toolID},
				map[string]any{"InstanceId": "ins-other-tool", "ToolId": "sdt-other"},
			}})
		case args[0] == "instance" && args[1] == "delete":
			deleted = append(deleted, args[2])
			return scriptedSucceeded(t, "instance.delete", map[string]any{"Deleted": 1})
		case args[0] == "instance" && args[1] == "get":
			return scriptedFailed(t, "instance.get", "INSTANCE_NOT_FOUND", "not_found")
		default:
			t.Fatalf("unexpected call %v", args)
			return testutil.CommandResult{}
		}
	}}
	if err := cleanupInstancesByTool(context.Background(), runner, toolID); err != nil {
		t.Fatalf("cleanupInstancesByTool: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "ins-this-run" {
		t.Fatalf("deleted = %v, want only the run's instance", deleted)
	}
}

func TestCleanupInstancesByToolReportsLookupFailure(t *testing.T) {
	runner := &scriptedRunner{respond: func(args []string) testutil.CommandResult {
		return scriptedFailed(t, "instance.list", "InternalError", "server_error")
	}}
	err := cleanupInstancesByTool(context.Background(), runner, "sdt-this-run")
	if err == nil || !strings.Contains(err.Error(), "list instances") {
		t.Fatalf("error = %v, want a lookup failure that reports cleanup is unverified", err)
	}
}

func TestExpectToolGonePollsUntilGone(t *testing.T) {
	reads := 0
	runner := &scriptedRunner{respond: func(args []string) testutil.CommandResult {
		reads++
		if reads == 1 {
			return scriptedSucceeded(t, "tool.get", map[string]any{"ToolId": "sdt-gone", "Status": "DELETING"})
		}
		return scriptedFailed(t, "tool.get", "TOOL_NOT_FOUND", "not_found")
	}}
	if err := expectToolGone(context.Background(), runner, "sdt-gone", time.Minute, time.Millisecond); err != nil {
		t.Fatalf("expectToolGone: %v", err)
	}
	if reads != 2 {
		t.Fatalf("tool get calls = %d, want a poll followed by the not_found read", reads)
	}
}

func TestExpectToolGoneReportsPersistentTool(t *testing.T) {
	runner := &scriptedRunner{respond: func(args []string) testutil.CommandResult {
		return scriptedSucceeded(t, "tool.get", map[string]any{"ToolId": "sdt-stuck", "Status": "DELETING"})
	}}
	err := expectToolGone(context.Background(), runner, "sdt-stuck", 0, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "still present") {
		t.Fatalf("error = %v, want the observed DELETING status", err)
	}
}

func TestPolicyCleanupRecoveryPaths(t *testing.T) {
	const name = "agr-issue180-1700000000000000000"
	runner := &scriptedRunner{respond: func(args []string) testutil.CommandResult {
		switch args[1] {
		case "list":
			return scriptedSucceeded(t, "pre-cache-image-cleanup-policy.list", map[string]any{"CleanupPolicySet": []any{
				map[string]any{"CleanupPolicyId": "pccp-this-run", "Name": name},
				map[string]any{"CleanupPolicyId": "pccp-other", "Name": "agr-issue180-other"},
			}})
		case "delete":
			return scriptedSucceeded(t, "pre-cache-image-cleanup-policy.delete", map[string]any{})
		default:
			t.Fatalf("unexpected call %v", args)
			return testutil.CommandResult{}
		}
	}}
	id, err := findPolicyByName(context.Background(), runner, name, `[{"Name":"name","Values":["`+name+`"]}]`)
	if err != nil {
		t.Fatalf("findPolicyByName: %v", err)
	}
	if id != "pccp-this-run" {
		t.Fatalf("policy ID = %q, want pccp-this-run", id)
	}
	if err := deletePolicy(context.Background(), runner, id); err != nil {
		t.Fatalf("deletePolicy: %v", err)
	}

	failing := &scriptedRunner{respond: func(args []string) testutil.CommandResult {
		return scriptedFailed(t, "pre-cache-image-cleanup-policy.list", "InternalError", "server_error")
	}}
	if _, err := findPolicyByName(context.Background(), failing, name, "[]"); err == nil {
		t.Fatal("a failed policy lookup must report cleanup is unverified")
	}
}

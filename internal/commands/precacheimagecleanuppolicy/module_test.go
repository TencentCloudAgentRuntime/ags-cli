package precacheimagecleanuppolicy_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/precacheimagecleanuppolicy"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/precacheimagecleanuppolicy/create"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/precacheimagecleanuppolicy/delete"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/precacheimagecleanuppolicy/list"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/precacheimagecleanuppolicy/update"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

type fakeControlPlane struct {
	action   string
	request  map[string]any
	response any
	err      error
	calls    int
}

func (f *fakeControlPlane) Call(_ context.Context, action string, request map[string]any) (any, error) {
	f.action = action
	f.request = request
	f.calls++
	return f.response, f.err
}

func createDescriptor() apicli.APIDescriptor { return create.APIDescriptor() }
func updateDescriptor() apicli.APIDescriptor { return update.APIDescriptor() }
func listDescriptor() apicli.APIDescriptor   { return list.APIDescriptor() }
func deleteDescriptor() apicli.APIDescriptor { return delete.APIDescriptor() }

func TestListModuleDocumentsOneFilterFlag(t *testing.T) {
	descriptor := listDescriptor()
	module := precacheimagecleanuppolicy.NewModule(descriptor)
	var filters int
	for _, flag := range module.Descriptor.Spec.Flags {
		if flag.Name != "filters" {
			continue
		}
		filters++
		if !slices.Equal(flag.Aliases, []string{"filter"}) {
			t.Fatalf("filters aliases = %v, want [filter]", flag.Aliases)
		}
		if !flag.HideAliases || !flag.RejectAliasConflicts {
			t.Fatalf("filters flag = %+v, want hidden alias and rejected conflicts", flag)
		}
	}
	if filters != 1 {
		t.Fatalf("module declares %d filters flags, want 1", filters)
	}
	// The override must not rewrite the generated descriptor that other
	// renderers hold on to.
	for _, flag := range descriptor.Spec.Flags {
		if flag.HideAliases || flag.RejectAliasConflicts {
			t.Fatalf("NewModule mutated the generated descriptor: %+v", flag)
		}
	}
}

func runDescriptor(t *testing.T, descriptor apicli.APIDescriptor, cp *fakeControlPlane, argValues map[string]string, flags map[string]command.FlagValue) (*command.Result, error) {
	t.Helper()
	runtime, err := precacheimagecleanuppolicy.NewModule(descriptor).Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return runtime.Handler.Run(t.Context(), command.Request{ArgValues: argValues, Flags: flags})
}

func TestValidateIdleTTL(t *testing.T) {
	cp := &fakeControlPlane{response: cleanupPolicyResponse("pccp-a")}
	for _, ttl := range []string{"30m", "12h", "3d", "100d", "1m"} {
		t.Run("valid "+ttl, func(t *testing.T) {
			cp.calls = 0
			if _, err := runDescriptor(t, createDescriptor(), cp, nil, map[string]command.FlagValue{
				"name":     {Name: "name", Type: command.FlagString, String: "demo", Changed: true},
				"idle-ttl": {Name: "idle-ttl", Type: command.FlagString, String: ttl, Changed: true},
			}); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if cp.calls != 1 || cp.request["IdleTTL"] != ttl {
				t.Fatalf("request = %#v", cp.request)
			}
		})
	}
	for _, ttl := range []string{"0d", "1.5h", "1h30m", "1s", "3", "m", "-3d", " 3d", "3 d"} {
		t.Run("invalid "+ttl, func(t *testing.T) {
			cp.calls = 0
			_, err := runDescriptor(t, createDescriptor(), cp, nil, map[string]command.FlagValue{
				"name":     {Name: "name", Type: command.FlagString, String: "demo", Changed: true},
				"idle-ttl": {Name: "idle-ttl", Type: command.FlagString, String: ttl, Changed: true},
			})
			var cliErr *output.CLIError
			if !errors.As(err, &cliErr) || cliErr.Failure.Code != "INVALID_IDLE_TTL" {
				t.Fatalf("error = %v, want INVALID_IDLE_TTL", err)
			}
			if !strings.Contains(cliErr.Failure.Message, ttl) || !strings.Contains(cliErr.Failure.Hint, "--idle-ttl 30m") {
				t.Fatalf("failure is not actionable: %+v", cliErr.Failure)
			}
			if cp.calls != 0 {
				t.Fatal("invalid TTL caused an API call")
			}
		})
	}
}

func TestValidateIdleTTLFromRequest(t *testing.T) {
	cp := &fakeControlPlane{response: cleanupPolicyResponse("pccp-a")}
	_, err := runDescriptor(t, createDescriptor(), cp, nil, map[string]command.FlagValue{
		"request": {Name: "request", Type: command.FlagString, String: `{"Name":"demo","IdleTTL":"90s"}`, Changed: true},
	})
	var cliErr *output.CLIError
	if !errors.As(err, &cliErr) || cliErr.Failure.Code != "INVALID_IDLE_TTL" {
		t.Fatalf("error = %v, want INVALID_IDLE_TTL", err)
	}
	if cp.calls != 0 {
		t.Fatal("invalid request TTL caused an API call")
	}
}

func TestUpdateRequiresOneField(t *testing.T) {
	cp := &fakeControlPlane{response: cleanupPolicyResponse("pccp-a")}
	for _, flags := range []map[string]command.FlagValue{
		{},
		{"request": {Name: "request", Type: command.FlagString, String: `{"CleanupPolicyId":"pccp-a"}`, Changed: true}},
		{"request": {Name: "request", Type: command.FlagString, String: `{"CleanupPolicyId":"pccp-a","Name":null,"Enabled":null,"IdleTTL":null,"RegistryHost":null,"RepoPattern":null}`, Changed: true}},
	} {
		_, err := runDescriptor(t, updateDescriptor(), cp, map[string]string{"cleanup-policy-id": "pccp-a"}, flags)
		var cliErr *output.CLIError
		if !errors.As(err, &cliErr) || cliErr.Failure.Code != "MISSING_REQUIRED_FLAG" {
			t.Fatalf("error = %v, want MISSING_REQUIRED_FLAG", err)
		}
		if !strings.Contains(cliErr.Failure.Message, "--enabled") {
			t.Fatalf("failure does not list the patch fields: %+v", cliErr.Failure)
		}
	}
	cp.calls = 0
	if _, err := runDescriptor(t, updateDescriptor(), cp, map[string]string{"cleanup-policy-id": "pccp-a"}, map[string]command.FlagValue{
		"enabled": {Name: "enabled", Type: command.FlagBool, Bool: false, Changed: true},
	}); err != nil {
		t.Fatalf("false-only patch: %v", err)
	}
	if cp.calls != 1 || cp.request["Enabled"] != false {
		t.Fatalf("request = %#v", cp.request)
	}
}

func TestResponseValidation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		descriptor apicli.APIDescriptor
		argValues  map[string]string
		flags      map[string]command.FlagValue
		response   any
	}{
		{
			name:       "create missing id",
			descriptor: createDescriptor(),
			flags: map[string]command.FlagValue{
				"name":     {Name: "name", Type: command.FlagString, String: "demo", Changed: true},
				"idle-ttl": {Name: "idle-ttl", Type: command.FlagString, String: "3d", Changed: true},
			},
			response: map[string]any{"RequestId": "request-create"},
		},
		{
			name:       "update empty policy",
			descriptor: updateDescriptor(),
			argValues:  map[string]string{"cleanup-policy-id": "pccp-a"},
			flags:      map[string]command.FlagValue{"enabled": {Name: "enabled", Type: command.FlagBool, Changed: true}},
			response:   map[string]any{"CleanupPolicy": map[string]any{}, "RequestId": "request-update"},
		},
		{
			name:       "delete missing request id",
			descriptor: deleteDescriptor(),
			argValues:  map[string]string{"cleanup-policy-id": "pccp-a"},
			response:   map[string]any{},
		},
		{
			name:       "list malformed set",
			descriptor: listDescriptor(),
			response:   map[string]any{"CleanupPolicySet": []any{nil}, "TotalCount": 1, "RequestId": "request-list"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cp := &fakeControlPlane{response: tc.response}
			_, err := runDescriptor(t, tc.descriptor, cp, tc.argValues, tc.flags)
			if tc.name == "list malformed set" {
				// The CLI runner classifies decode errors from the shared response
				// helpers as INTERNAL_ERROR; the handler itself returns a plain error.
				if err == nil || !strings.Contains(err.Error(), "CleanupPolicySet[0]") {
					t.Fatalf("error = %v, want a CleanupPolicySet decode error", err)
				}
			} else {
				var cliErr *output.CLIError
				if !errors.As(err, &cliErr) || cliErr.Failure.Code != "INTERNAL_ERROR" {
					t.Fatalf("error = %v, want INTERNAL_ERROR", err)
				}
			}
			if cp.calls != 1 {
				t.Fatalf("calls = %d", cp.calls)
			}
		})
	}
}

func TestTextRendering(t *testing.T) {
	for _, tc := range []struct {
		name       string
		descriptor apicli.APIDescriptor
		argValues  map[string]string
		flags      map[string]command.FlagValue
		response   any
		want       []string
		absent     []string
	}{
		{
			name:       "create",
			descriptor: createDescriptor(),
			flags: map[string]command.FlagValue{
				"name":     {Name: "name", Type: command.FlagString, String: "demo", Changed: true},
				"idle-ttl": {Name: "idle-ttl", Type: command.FlagString, String: "3d", Changed: true},
			},
			response: cleanupPolicyResponse("pccp-a"),
			want:     []string{"CleanupPolicyId:", "pccp-a", "Name:", "demo", "Enabled:", "true", "IdleTTL:", "3d", "RegistryHost:", "example.invalid", "RepoPattern:", "^demo$"},
		},
		{
			name:       "list rows",
			descriptor: listDescriptor(),
			response: map[string]any{"CleanupPolicySet": []any{
				map[string]any{"CleanupPolicyId": "pccp-a", "Name": "demo", "Enabled": true, "IdleTTL": "3d", "RegistryHost": "example.invalid", "RepoPattern": "^demo$"},
				map[string]any{"CleanupPolicyId": "pccp-b", "Name": "other", "Enabled": false, "IdleTTL": "12h", "RegistryHost": "", "RepoPattern": "-"},
				map[string]any{"CleanupPolicyId": "pccp-c", "Name": "bare", "Enabled": true, "IdleTTL": "1h"},
			}, "TotalCount": 3, "RequestId": "request-list"},
			want: []string{"ID", "NAME", "ENABLED", "IDLE TTL", "REGISTRY HOST", "REPO PATTERN", "pccp-a", "demo", "true", "3d", "example.invalid", "^demo$", "pccp-b", "other", "false", "12h", "-", "pccp-c", "bare", "Total: 3"},
		},
		{
			name:       "list empty",
			descriptor: listDescriptor(),
			response:   map[string]any{"CleanupPolicySet": []any{}, "TotalCount": 0, "RequestId": "request-list"},
			want:       []string{"No pre-cache image cleanup policies found", "Total: 0"},
			absent:     []string{"IDLE TTL", "REGISTRY HOST", "REPO PATTERN"},
		},
		{
			name:       "delete",
			descriptor: deleteDescriptor(),
			argValues:  map[string]string{"cleanup-policy-id": "pccp-a"},
			response:   map[string]any{"RequestId": "request-delete"},
			want:       []string{"Pre-cache image cleanup policy deleted: pccp-a"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := runDescriptor(t, tc.descriptor, &fakeControlPlane{response: tc.response}, tc.argValues, tc.flags)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			var buf bytes.Buffer
			result.Text(&buf)
			for _, fragment := range tc.want {
				if !strings.Contains(buf.String(), fragment) {
					t.Errorf("text missing %q:\n%s", fragment, buf.String())
				}
			}
			for _, fragment := range tc.absent {
				if strings.Contains(buf.String(), fragment) {
					t.Errorf("text unexpectedly contains %q:\n%s", fragment, buf.String())
				}
			}
			if !strings.HasPrefix(tc.name, "list ") {
				if len(result.Effects) != 1 || result.Effects[0].Resource != "pre-cache-image-cleanup-policy" {
					t.Errorf("effects = %#v", result.Effects)
				}
			}
		})
	}
}

func TestEffectsCarryPolicyID(t *testing.T) {
	for _, tc := range []struct {
		name       string
		descriptor apicli.APIDescriptor
		argValues  map[string]string
		flags      map[string]command.FlagValue
		kind       string
		response   any
	}{
		{"create", createDescriptor(), nil, map[string]command.FlagValue{
			"name":     {Name: "name", Type: command.FlagString, String: "demo", Changed: true},
			"idle-ttl": {Name: "idle-ttl", Type: command.FlagString, String: "3d", Changed: true},
		}, "create", cleanupPolicyResponse("pccp-a")},
		{"update", updateDescriptor(), map[string]string{"cleanup-policy-id": "pccp-a"}, map[string]command.FlagValue{
			"enabled": {Name: "enabled", Type: command.FlagBool, Changed: true},
		}, "update", cleanupPolicyResponse("pccp-a")},
		{"delete", deleteDescriptor(), map[string]string{"cleanup-policy-id": "pccp-a"}, nil, "delete", map[string]any{"RequestId": "request-delete"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := runDescriptor(t, tc.descriptor, &fakeControlPlane{response: tc.response}, tc.argValues, tc.flags)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if len(result.Effects) != 1 {
				t.Fatalf("effects = %#v", result.Effects)
			}
			effect := result.Effects[0]
			if effect.Kind != tc.kind || effect.Resource != "pre-cache-image-cleanup-policy" || effect.Id != "pccp-a" {
				t.Fatalf("effect = %#v", effect)
			}
		})
	}
}

func cleanupPolicyResponse(id string) map[string]any {
	return map[string]any{
		"CleanupPolicy": map[string]any{
			"CleanupPolicyId": id,
			"Name":            "demo",
			"Enabled":         true,
			"IdleTTL":         "3d",
			"RegistryHost":    "example.invalid",
			"RepoPattern":     "^demo$",
		},
		"RequestId": "request-write",
	}
}

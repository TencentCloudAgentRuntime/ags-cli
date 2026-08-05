package identity_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	identitycreate "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/identity/create"
	identitydelete "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/identity/delete"
	identityget "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/identity/get"
	identitylist "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/identity/list"
	identitytokencreate "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/identity/token/create"
	identityupdate "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/identity/update"
)

// fakeCP implements identity.ControlPlane for testing.
type fakeCP struct {
	action  string
	request map[string]any
	resp    any
	err     error
}

func (f *fakeCP) Call(_ context.Context, action string, request map[string]any) (any, error) {
	f.action = action
	f.request = request
	return f.resp, f.err
}

// --- identity create ---

func TestIdentityCreate(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{"WorkloadIdentityId": "wi-abc"}}
	mod := identitycreate.Module()
	rt, err := mod.Build(command.Deps{ControlPlane: fake})
	if err != nil {
		t.Fatal(err)
	}
	result, err := rt.Handler.Run(context.Background(), command.Request{
		Flags: map[string]command.FlagValue{
			"name":                       {String: "test-agent", Changed: true},
			"allowed-oauth2-return-urls": {Strings: []string{"https://cb.example.com"}, Changed: true},
			"tags":                       {Strings: []string{"env=prod", "team=ai"}, Changed: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "action", fake.action, "CreateWorkloadIdentity")
	assertEqual(t, "Name", fake.request["Name"], "test-agent")
	urls := fake.request["AllowedOAuth2ReturnUrls"].([]string)
	assertEqual(t, "urls[0]", urls[0], "https://cb.example.com")
	tags := fake.request["Tags"].([]map[string]string)
	assertEqual(t, "tag count", fmt.Sprint(len(tags)), "2")
	assertEqual(t, "tag[0].Key", tags[0]["Key"], "env")
	data := result.Data.(map[string]any)
	assertEqual(t, "WorkloadIdentityId", data["WorkloadIdentityId"], "wi-abc")
}

// --- identity list ---

func TestIdentityList(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{
		"WorkloadIdentitySet": []any{
			map[string]any{"WorkloadIdentityId": "wi-1", "Name": "agent-1"},
		},
		"TotalCount": 1,
	}}
	mod := identitylist.Module()
	rt, _ := mod.Build(command.Deps{ControlPlane: fake})
	result, err := rt.Handler.Run(context.Background(), command.Request{
		Flags: map[string]command.FlagValue{
			"identity-ids": {String: "wi-1,wi-2", Changed: true},
			"filter":       {Strings: []string{"tag-key=env"}, Changed: true},
			"limit":        {Int: 10, Changed: true},
			"offset":       {Int: 0},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "action", fake.action, "DescribeWorkloadIdentityList")
	ids := fake.request["WorkloadIdentityIds"].([]string)
	assertEqual(t, "ids count", fmt.Sprint(len(ids)), "2")
	data := result.Data.(map[string]any)
	set := data["WorkloadIdentitySet"].([]any)
	assertEqual(t, "set count", fmt.Sprint(len(set)), "1")
}

// --- identity get ---

func TestIdentityGet(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{
		"WorkloadIdentitySet": []any{
			map[string]any{"WorkloadIdentityId": "wi-x", "Name": "my-id"},
		},
	}}
	mod := identityget.Module()
	rt, _ := mod.Build(command.Deps{ControlPlane: fake})
	result, err := rt.Handler.Run(context.Background(), command.Request{
		Args:  []string{"wi-x"},
		Flags: map[string]command.FlagValue{},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "action", fake.action, "DescribeWorkloadIdentityList")
	ids := fake.request["WorkloadIdentityIds"].([]string)
	assertEqual(t, "id", ids[0], "wi-x")
	data := result.Data.(map[string]any)
	assertEqual(t, "Name", data["Name"], "my-id")
}

func TestIdentityGetNotFound(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{"WorkloadIdentitySet": []any{}}}
	mod := identityget.Module()
	rt, _ := mod.Build(command.Deps{ControlPlane: fake})
	_, err := rt.Handler.Run(context.Background(), command.Request{
		Args:  []string{"wi-notexist"},
		Flags: map[string]command.FlagValue{},
	})
	if err == nil {
		t.Fatal("expected error for not found")
	}
}

// --- identity update ---

func TestIdentityUpdate(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{}}
	mod := identityupdate.Module()
	rt, _ := mod.Build(command.Deps{ControlPlane: fake})
	_, err := rt.Handler.Run(context.Background(), command.Request{
		Args: []string{"wi-upd"},
		Flags: map[string]command.FlagValue{
			"name":                       {String: "new-name", Changed: true},
			"allowed-oauth2-return-urls": {Strings: []string{"https://new.cb"}, Changed: true},
			"tags":                       {Strings: []string{"k=v"}, Changed: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "action", fake.action, "UpdateWorkloadIdentity")
	assertEqual(t, "WorkloadIdentityId", fake.request["WorkloadIdentityId"], "wi-upd")
	assertEqual(t, "Name", fake.request["Name"], "new-name")
}

func TestIdentityUpdateNoFields(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{}}
	mod := identityupdate.Module()
	rt, _ := mod.Build(command.Deps{ControlPlane: fake})
	_, err := rt.Handler.Run(context.Background(), command.Request{
		Args: []string{"wi-upd"},
		Flags: map[string]command.FlagValue{
			"name":                       {},
			"allowed-oauth2-return-urls": {},
			"tags":                       {},
		},
	})
	if err == nil {
		t.Fatal("expected error when no fields provided")
	}
}

// --- identity delete ---

func TestIdentityDelete(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{}}
	mod := identitydelete.Module()
	rt, _ := mod.Build(command.Deps{ControlPlane: fake})
	result, err := rt.Handler.Run(context.Background(), command.Request{
		Args:  []string{"wi-del"},
		Flags: map[string]command.FlagValue{},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "action", fake.action, "DeleteWorkloadIdentity")
	assertEqual(t, "WorkloadIdentityId", fake.request["WorkloadIdentityId"], "wi-del")
	data := result.Data.(map[string]any)
	assertEqual(t, "Deleted", fmt.Sprint(data["Deleted"]), "true")
}

// --- identity token create ---

func TestIdentityTokenCreate(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{"WorkloadAccessToken": "eyJ-long-token-here"}}
	mod := identitytokencreate.Module()
	rt, _ := mod.Build(command.Deps{ControlPlane: fake})
	result, err := rt.Handler.Run(context.Background(), command.Request{
		Flags: map[string]command.FlagValue{
			"identity-id": {String: "wi-tok", Changed: true},
			"user-id":     {String: "user-123", Changed: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "action", fake.action, "CreateWorkloadAccessTokenForUserId")
	assertEqual(t, "WorkloadIdentityId", fake.request["WorkloadIdentityId"], "wi-tok")
	assertEqual(t, "UserId", fake.request["UserId"], "user-123")
	data := result.Data.(map[string]any)
	if data["WorkloadAccessToken"] == nil {
		t.Error("expected WorkloadAccessToken in result")
	}
}

// --- helper ---

func assertEqual(t *testing.T, field string, got, want any) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%s = %v, want %v", field, got, want)
	}
}

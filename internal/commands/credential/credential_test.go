package credential_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	credoauth2acquire "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/credential/oauth2/acquire"
	credoauth2complete "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/credential/oauth2/complete"
	credprovidercreate "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/credential/provider/create"
	credproviderdelete "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/credential/provider/delete"
	credproviderget "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/credential/provider/get"
	credproviderlist "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/credential/provider/list"
	credproviderupdate "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/credential/provider/update"
	credsecretdelete "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/credential/secret/delete"
	credsecretget "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/credential/secret/get"
	credsecretlist "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/credential/secret/list"
	credsecretset "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/credential/secret/set"
)

type fakeCP struct {
	action  string
	request map[string]any
	resp    any
}

func (f *fakeCP) Call(_ context.Context, action string, request map[string]any) (any, error) {
	f.action = action
	f.request = request
	return f.resp, nil
}

// --- provider create ---
func TestProviderCreate(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{"ProviderId": "agc-123"}}
	rt, _ := credprovidercreate.Module().Build(command.Deps{ControlPlane: fake})
	_, err := rt.Handler.Run(context.Background(), command.Request{Flags: map[string]command.FlagValue{
		"name": {String: "my-p", Changed: true}, "type": {String: "OAuth2", Changed: true},
		"description":     {String: "desc", Changed: true},
		"provider-config": {String: `[{"Key":"client_id","Value":"x"}]`, Changed: true},
		"tags":            {Strings: []string{"k=v"}, Changed: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "action", fake.action, "CreateCredentialProvider")
	assertEqual(t, "Name", fake.request["Name"], "my-p")
	assertEqual(t, "Type", fake.request["Type"], "OAuth2")
}

// --- provider list ---
func TestProviderList(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{"ProviderSet": []any{}, "TotalCount": 0}}
	rt, _ := credproviderlist.Module().Build(command.Deps{ControlPlane: fake})
	_, err := rt.Handler.Run(context.Background(), command.Request{Flags: map[string]command.FlagValue{
		"provider-ids": {String: "agc-1", Changed: true},
		"filter":       {Strings: []string{"type=OAuth2"}, Changed: true},
		"limit":        {Int: 20}, "offset": {Int: 0},
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "action", fake.action, "DescribeCredentialProviderList")
	ids := fake.request["ProviderIds"].([]string)
	assertEqual(t, "ids[0]", ids[0], "agc-1")
}

// --- provider get ---
func TestProviderGet(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{"ProviderSet": []any{map[string]any{"ProviderId": "agc-x", "Name": "test"}}, "TotalCount": 1}}
	rt, _ := credproviderget.Module().Build(command.Deps{ControlPlane: fake})
	result, err := rt.Handler.Run(context.Background(), command.Request{Args: []string{"agc-x"}, Flags: map[string]command.FlagValue{}})
	if err != nil {
		t.Fatal(err)
	}
	data := result.Data.(map[string]any)
	assertEqual(t, "ProviderId", data["ProviderId"], "agc-x")
}

func TestProviderGetNotFound(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{"ProviderSet": []any{}}}
	rt, _ := credproviderget.Module().Build(command.Deps{ControlPlane: fake})
	_, err := rt.Handler.Run(context.Background(), command.Request{Args: []string{"agc-nope"}, Flags: map[string]command.FlagValue{}})
	if err == nil {
		t.Fatal("expected not found error")
	}
}

// --- provider update ---
func TestProviderUpdate(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{}}
	rt, _ := credproviderupdate.Module().Build(command.Deps{ControlPlane: fake})
	_, err := rt.Handler.Run(context.Background(), command.Request{Args: []string{"agc-upd"}, Flags: map[string]command.FlagValue{
		"name": {String: "new", Changed: true}, "description": {}, "provider-config": {}, "tags": {}, "status": {},
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "action", fake.action, "UpdateCredentialProvider")
	assertEqual(t, "ProviderId", fake.request["ProviderId"], "agc-upd")
}

func TestProviderUpdateNoFields(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{}}
	rt, _ := credproviderupdate.Module().Build(command.Deps{ControlPlane: fake})
	_, err := rt.Handler.Run(context.Background(), command.Request{Args: []string{"agc-x"}, Flags: map[string]command.FlagValue{
		"name": {}, "description": {}, "provider-config": {}, "tags": {}, "status": {},
	}})
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- provider delete ---
func TestProviderDelete(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{}}
	rt, _ := credproviderdelete.Module().Build(command.Deps{ControlPlane: fake})
	_, err := rt.Handler.Run(context.Background(), command.Request{Args: []string{"agc-del"}, Flags: map[string]command.FlagValue{}})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "action", fake.action, "DeleteCredentialProvider")
	assertEqual(t, "ProviderId", fake.request["ProviderId"], "agc-del")
}

// --- secret set ---
func TestSecretSet(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{}}
	rt, _ := credsecretset.Module().Build(command.Deps{ControlPlane: fake})
	_, err := rt.Handler.Run(context.Background(), command.Request{Flags: map[string]command.FlagValue{
		"credential-provider-id": {String: "agc-s", Changed: true},
		"user-id":                {String: "u1", Changed: true},
		"secret":                 {String: "pw123", Changed: true},
		"scope":                  {String: "read:user", Changed: true},
		"overwrite-allowed":      {Bool: true},
		"from-stdin":             {}, "metadata": {},
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "action", fake.action, "SetManagedSecret")
	assertEqual(t, "Secret", fake.request["Secret"], "pw123")
	assertEqual(t, "OverwriteAllowed", fmt.Sprint(fake.request["OverwriteAllowed"]), "true")
}

func TestSecretSetFromStdin(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{}}
	rt, _ := credsecretset.Module().Build(command.Deps{ControlPlane: fake})
	_, err := rt.Handler.Run(context.Background(), command.Request{
		Stdin: strings.NewReader("stdin-pw"),
		Flags: map[string]command.FlagValue{
			"credential-provider-id": {String: "agc-s", Changed: true},
			"user-id":                {String: "u2", Changed: true},
			"secret":                 {}, "from-stdin": {Bool: true},
			"scope": {}, "overwrite-allowed": {}, "metadata": {},
		}})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "Secret", fake.request["Secret"], "stdin-pw")
}

func TestSecretSetFromStdinRejectsOversize(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{}}
	rt, _ := credsecretset.Module().Build(command.Deps{ControlPlane: fake})
	// 65537 bytes — one byte past the 65536-byte cap. Must not be silently
	// truncated and submitted; the command should return a usage error.
	oversize := strings.Repeat("x", 65537)
	_, err := rt.Handler.Run(context.Background(), command.Request{
		Stdin: strings.NewReader(oversize),
		Flags: map[string]command.FlagValue{
			"credential-provider-id": {String: "agc-s", Changed: true},
			"user-id":                {String: "u2", Changed: true},
			"secret":                 {}, "from-stdin": {Bool: true},
			"scope": {}, "overwrite-allowed": {}, "metadata": {},
		}})
	if err == nil {
		t.Fatal("expected error for oversize secret")
	}
	if fake.action != "" {
		t.Fatalf("command should not have called ControlPlane, action=%q", fake.action)
	}
}

func TestSecretSetFromStdinAtLimit(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{}}
	rt, _ := credsecretset.Module().Build(command.Deps{ControlPlane: fake})
	// Exactly 65536 bytes — the boundary; must be accepted, not rejected.
	atLimit := strings.Repeat("x", 65536)
	_, err := rt.Handler.Run(context.Background(), command.Request{
		Stdin: strings.NewReader(atLimit),
		Flags: map[string]command.FlagValue{
			"credential-provider-id": {String: "agc-s", Changed: true},
			"user-id":                {String: "u2", Changed: true},
			"secret":                 {}, "from-stdin": {Bool: true},
			"scope": {}, "overwrite-allowed": {}, "metadata": {},
		}})
	if err != nil {
		t.Fatalf("expected accept at limit, got error: %v", err)
	}
	if got, ok := fake.request["Secret"].(string); !ok || len(got) != 65536 {
		t.Fatalf("Secret len = %v, want 65536", len(got))
	}
}

func TestSecretSetConflict(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{}}
	rt, _ := credsecretset.Module().Build(command.Deps{ControlPlane: fake})
	_, err := rt.Handler.Run(context.Background(), command.Request{
		Stdin: strings.NewReader("stdin-pw"),
		Flags: map[string]command.FlagValue{
			"credential-provider-id": {String: "agc-s", Changed: true},
			"user-id":                {String: "u1", Changed: true},
			"secret":                 {String: "flag-pw", Changed: true},
			"from-stdin":             {Bool: true},
			"scope":                  {}, "overwrite-allowed": {}, "metadata": {},
		}})
	if err == nil {
		t.Fatal("expected error for --secret + --from-stdin conflict")
	}
	if fake.action != "" {
		t.Fatalf("command should not have calledControlPlane, action=%q", fake.action)
	}
}

func TestSecretSetMissing(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{}}
	rt, _ := credsecretset.Module().Build(command.Deps{ControlPlane: fake})
	_, err := rt.Handler.Run(context.Background(), command.Request{Flags: map[string]command.FlagValue{
		"credential-provider-id": {String: "agc-s", Changed: true},
		"user-id":                {String: "u1", Changed: true},
		"secret":                 {}, "from-stdin": {}, "scope": {}, "overwrite-allowed": {}, "metadata": {},
	}})
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- secret list ---
func TestSecretList(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{"ManagedSecretSet": []any{}, "TotalCount": 0}}
	rt, _ := credsecretlist.Module().Build(command.Deps{ControlPlane: fake})
	_, err := rt.Handler.Run(context.Background(), command.Request{Flags: map[string]command.FlagValue{
		"credential-provider-id": {String: "agc-s", Changed: true},
		"user-ids":               {String: "u1,u2", Changed: true},
		"filter":                 {Strings: []string{"scope=read:user"}, Changed: true},
		"limit":                  {Int: 20}, "offset": {Int: 0},
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "action", fake.action, "DescribeManagedSecretList")
	uids := fake.request["UserIds"].([]string)
	assertEqual(t, "uids count", fmt.Sprint(len(uids)), "2")
}

// --- secret get ---
func TestSecretGet(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{"Secret": "real-secret", "Metadata": []any{}}}
	rt, _ := credsecretget.Module().Build(command.Deps{ControlPlane: fake})
	result, err := rt.Handler.Run(context.Background(), command.Request{Flags: map[string]command.FlagValue{
		"credential-provider-id": {String: "agc-g", Changed: true},
		"token":                  {String: "eyJ-token", Changed: true},
		"scope":                  {String: "read:user", Changed: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "action", fake.action, "GetManagedSecret")
	assertEqual(t, "WorkloadIdentityToken", fake.request["WorkloadIdentityToken"], "eyJ-token")
	data := result.Data.(map[string]any)
	assertEqual(t, "Secret", data["Secret"], "real-secret")
}

// --- secret delete ---
func TestSecretDelete(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{}}
	rt, _ := credsecretdelete.Module().Build(command.Deps{ControlPlane: fake})
	_, err := rt.Handler.Run(context.Background(), command.Request{Flags: map[string]command.FlagValue{
		"credential-provider-id": {String: "agc-d", Changed: true},
		"user-id":                {String: "u1", Changed: true},
		"scope":                  {String: "read:user", Changed: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "action", fake.action, "DeleteManagedSecret")
	assertEqual(t, "Scope", fake.request["Scope"], "read:user")
}

func TestSecretDeleteAllScopes(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{}}
	rt, _ := credsecretdelete.Module().Build(command.Deps{ControlPlane: fake})
	_, err := rt.Handler.Run(context.Background(), command.Request{Flags: map[string]command.FlagValue{
		"credential-provider-id": {String: "agc-d", Changed: true},
		"user-id":                {String: "u1", Changed: true},
		"scope":                  {},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, hasScope := fake.request["Scope"]; hasScope {
		t.Error("expected no Scope when flag not set")
	}
}

// --- oauth2 acquire ---
func TestOAuth2Acquire(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{
		"AuthorizationUrl": "https://auth.example.com/...",
		"SessionUri":       "urn:ietf:params:oauth:request_uri:xxx",
		"SessionStatus":    "IN_PROGRESS",
	}}
	rt, _ := credoauth2acquire.Module().Build(command.Deps{ControlPlane: fake})
	result, err := rt.Handler.Run(context.Background(), command.Request{Flags: map[string]command.FlagValue{
		"token":                  {String: "eyJ-tok", Changed: true},
		"credential-provider-id": {String: "agc-oauth", Changed: true},
		"flow":                   {String: "AUTHORIZATION_CODE", Changed: true},
		"scopes":                 {Strings: []string{"read:user"}, Changed: true},
		"return-url":             {String: "https://cb.example.com", Changed: true},
		"custom-state":           {},
		"force-authentication":   {},
		"session-uri":            {},
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "action", fake.action, "AcquireOAuth2AccessToken")
	assertEqual(t, "CredentialProviderId", fake.request["CredentialProviderId"], "agc-oauth")
	assertEqual(t, "OAuth2Flow", fake.request["OAuth2Flow"], "AUTHORIZATION_CODE")
	data := result.Data.(map[string]any)
	assertEqual(t, "SessionStatus", data["SessionStatus"], "IN_PROGRESS")
}

// --- oauth2 complete ---
func TestOAuth2Complete(t *testing.T) {
	fake := &fakeCP{resp: map[string]any{}}
	rt, _ := credoauth2complete.Module().Build(command.Deps{ControlPlane: fake})
	_, err := rt.Handler.Run(context.Background(), command.Request{Flags: map[string]command.FlagValue{
		"session-uri": {String: "urn:ietf:xxx", Changed: true},
		"user-id":     {String: "user-1", Changed: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "action", fake.action, "CompleteOAuth2AccessTokenAuth")
	assertEqual(t, "SessionUri", fake.request["SessionUri"], "urn:ietf:xxx")
	assertEqual(t, "UserId", fake.request["UserId"], "user-1")
}

// --- helper ---
func assertEqual(t *testing.T, field string, got, want any) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%s = %v, want %v", field, got, want)
	}
}

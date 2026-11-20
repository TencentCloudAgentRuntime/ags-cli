package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
)

func TestEndpointCommandChannel(t *testing.T) {
	output, err := runAGR(t, "schema", "-o", "json")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	var schema struct {
		Data struct {
			Commands []struct{ Name string }
		}
	}
	if err := json.Unmarshal([]byte(output), &schema); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, command := range schema.Data.Commands {
		if command.Name == "endpoint" || strings.HasPrefix(command.Name, "endpoint.") {
			got = append(got, command.Name)
		}
	}
	if apimeta.BuildChannel == apimeta.Stable {
		if len(got) > 0 {
			t.Fatalf("stable schema exposes %v", got)
		}
		if _, err := runAGR(t, "endpoint", "list", "-o", "json"); err == nil {
			t.Fatal("stable accepted an Endpoint resource command")
		}
		return
	}
	want := []string{
		"endpoint", "endpoint.create", "endpoint.delete", "endpoint.get", "endpoint.list", "endpoint.metrics",
		"endpoint.target", "endpoint.target.candidates", "endpoint.target.create", "endpoint.target.delete",
		"endpoint.target.get", "endpoint.target.list", "endpoint.target.sync", "endpoint.target.update",
		"endpoint.token", "endpoint.token.create", "endpoint.token.list", "endpoint.token.revoke", "endpoint.update",
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("preview Endpoint commands = %v, want %v", got, want)
	}
}

func TestEndpointPreviewSignedRequests(t *testing.T) {
	if apimeta.BuildChannel != apimeta.Preview {
		t.Skip("requires a preview build")
	}
	for _, tc := range []struct {
		name, action, request, response string
		args                            []string
	}{
		{"clear target fields", "ModifyEndpointTarget",
			`{"EndpointId":"ep-test","TargetId":"ept-test","ExpectedRevision":4,"Description":"","PathAlias":"","VersionId":"rv-test"}`,
			`{"Target":{"TargetId":"ept-test","Revision":5,"VersionId":"rv-test"},"RequestId":"request-id"}`,
			[]string{"target", "update", "--endpoint-id", "ep-test", "--target-id", "ept-test",
				"--expected-revision", "4", "--description=", "--path-alias=", "--version-id", "rv-test"}},
		{"clear endpoint description", "ModifyEndpoint",
			`{"EndpointId":"ep-test","ExpectedRevision":4,"Description":""}`,
			`{"Endpoint":{"EndpointId":"ep-test","Revision":5,"Description":""},"RequestId":"request-id"}`,
			[]string{"update", "--endpoint-id", "ep-test", "--expected-revision", "4", "--description="}},
		{"permanent token", "CreateEndpointToken",
			`{"EndpointId":"ep-test","Name":"smoke","ExpiresIn":0}`,
			`{"TokenId":"eptk-test","Token":"example-token","RequestId":"request-id"}`,
			[]string{"token", "create", "--endpoint-id", "ep-test", "--name", "smoke", "--expires-in", "0"}},
		{"revoke token", "RevokeEndpointToken",
			`{"EndpointId":"ep-test","TokenId":"eptk-test"}`, `{"RequestId":"request-id"}`,
			[]string{"token", "revoke", "--endpoint-id", "ep-test", "--token-id", "eptk-test"}},
		{"metrics lists", "DescribeEndpointMetrics",
			`{"EndpointIds":["ep-test","ep-second"],"MetricNames":["InvocationCount","P95LatencyMs"]}`,
			`{"Results":[{"MetricName":"InvocationCount","Data":"{\"resultType\":\"vector\",\"result\":[]}"}],"RequestId":"request-id"}`,
			[]string{"metrics", "--endpoint-ids", "ep-test", "--endpoint-ids", "ep-second",
				"--metric-names", "InvocationCount", "--metric-names", "P95LatencyMs"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bodies := make(chan []byte, 1)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/" ||
					r.Header.Get("X-TC-Action") != tc.action || r.Header.Get("X-TC-Version") != "2025-09-20" ||
					!strings.HasPrefix(r.Header.Get("Authorization"), "TC3-HMAC-SHA256 ") {
					t.Errorf("unexpected Cloud API request for %s", tc.action)
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				bodies <- body
				_, _ = io.WriteString(w, `{"Response":`+tc.response+`}`)
			}))
			defer server.Close()
			args := append(append([]string{"endpoint"}, tc.args...), "-o", "json")
			result := operationalEventCLI(t, server, "", args...)
			if result.code != 0 {
				t.Fatalf("exit=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
			}
			response := operationalEventObject(t, result.stdout)
			if !reflect.DeepEqual(response["Data"], operationalEventObject(t, []byte(tc.response))) {
				t.Fatalf("CLI lost response fields: %s", result.stdout)
			}
			select {
			case body := <-bodies:
				if !reflect.DeepEqual(operationalEventObject(t, body), operationalEventObject(t, []byte(tc.request))) {
					t.Fatalf("signed body = %s, want %s", body, tc.request)
				}
			default:
				t.Fatal("no signed request")
			}
		})
	}
}

func TestEndpointPreviewRejectsIncompleteTokenIssuance(t *testing.T) {
	if apimeta.BuildChannel != apimeta.Preview {
		t.Skip("requires a preview build")
	}
	for _, tc := range []struct{ name, data, tokenID string }{
		{"RequestId only", `"RequestId":"issue-request"`, ""},
		{"without token", `"RequestId":"issue-request","TokenId":"eptk-test"`, "eptk-test"},
		{"without token ID", `"RequestId":"issue-request","Token":"test-secret"`, ""},
		{"blank token", `"RequestId":"issue-request","TokenId":"eptk-test","Token":" "`, "eptk-test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-TC-Action") != "CreateEndpointToken" {
					t.Error("unexpected Cloud API action")
				}
				_, _ = io.WriteString(w, `{"Response":{`+tc.data+`}}`)
			}))
			defer server.Close()
			call := operationalEventCLI(t, server, "", "endpoint", "token", "create",
				"--endpoint-id", "ep-fixture", "--name", "demo", "-o", "json")
			response := operationalEventObject(t, call.stdout)
			failure, _ := response["Failure"].(map[string]any)
			details, _ := failure["Details"].(map[string]any)
			tokenID, _ := details["TokenId"].(string)
			if call.code != 1 || failure["Code"] != "ENDPOINT_TOKEN_INCOMPLETE" ||
				details["RequestId"] != "issue-request" || tokenID != tc.tokenID {
				t.Fatalf("unexpected error for incomplete token response: exit=%d %s", call.code, call.stdout)
			}
			if strings.Contains(string(call.stdout)+string(call.stderr), "test-secret") {
				t.Fatal("partial response leaked the Token")
			}
		})
	}
}

func TestEndpointPreviewExamplesReachAPI(t *testing.T) {
	if apimeta.BuildChannel != apimeta.Preview {
		t.Skip("requires a preview build")
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"Response":{"RequestId":"mock","TokenId":"eptk-test","Token":"example-token"}}`)
	}))
	defer server.Close()
	for _, name := range []string{
		"create", "list", "get", "update", "delete", "metrics", "target.candidates", "target.create",
		"target.list", "target.get", "target.update", "target.delete", "target.sync", "token.create", "token.list", "token.revoke",
	} {
		t.Run(name, func(t *testing.T) {
			schema := schemaForCommand(t, "endpoint."+name)
			if len(schema.Examples) == 0 {
				t.Fatal("command requires a runnable example")
			}
			for _, example := range schema.Examples {
				args := strings.Fields(example)
				if len(args) < 3 || args[0] != "agr" || args[1] != "endpoint" {
					t.Fatalf("unexpected example %q", example)
				}
				result := operationalEventCLI(t, server, "", append(args[1:], "-o", "json")...)
				if result.code != 0 {
					t.Fatalf("%s exited %d: %s", example, result.code, result.stdout)
				}
			}
		})
	}
}

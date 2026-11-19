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
		{"permanent token", "CreateEndpointToken",
			`{"EndpointId":"ep-test","Name":"smoke","ExpiresIn":0}`,
			`{"TokenId":"eptk-test","Token":"example-token","RequestId":"request-id"}`,
			[]string{"token", "create", "--endpoint-id", "ep-test", "--name", "smoke", "--expires-in", "0"}},
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

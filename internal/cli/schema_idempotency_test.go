package cli

import (
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// TestRequestAcceptsClientToken pins the derivation that keeps reported
// idempotency tied to the request contract instead of a hand-edited list.
func TestRequestAcceptsClientToken(t *testing.T) {
	if requestAcceptsClientToken(nil) {
		t.Fatal("nil request schema must not claim client-token support")
	}
	request := &RequestSchema{Type: "object", Properties: map[string]PropertySchema{
		"VolumeName": {Type: "string", CliFlag: cliFlag("volume-name")},
	}}
	if requestAcceptsClientToken(request) {
		t.Fatal("request without ClientToken must not claim client-token support")
	}
	request.Properties[clientTokenProperty] = PropertySchema{Type: "string", CliFlag: cliFlag("client-token")}
	if !requestAcceptsClientToken(request) {
		t.Fatal("ClientToken request member not recognised")
	}
}

// TestIdempotencyHintFollowsSchema pins the user-visible consequence: the retry
// hint is driven by the declared idempotency, on retryable failures only.
func TestIdempotencyHintFollowsSchema(t *testing.T) {
	for _, tc := range []struct {
		name      string
		command   string
		retryable bool
		want      bool
	}{
		{name: "client token command", command: "tool.create", retryable: true, want: true},
		{name: "plain command", command: "tool.get", retryable: true, want: false},
		{name: "non retryable", command: "tool.create", retryable: false, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failure := &output.Failure{Code: "INTERNAL_ERROR", Retryable: tc.retryable, Hint: "Try again."}
			hinted := withIdempotencyHint(tc.command, failure)
			if got := strings.Contains(hinted.Hint, "--client-token"); got != tc.want {
				t.Errorf("client-token hint = %v, want %v (hint=%q)", got, tc.want, hinted.Hint)
			}
		})
	}
}

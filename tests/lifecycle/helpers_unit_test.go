package lifecycle

import (
	"encoding/json"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/tests/testutil"
)

func TestCreatedResourceID(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		dataKey string
		want    string
	}{
		{
			name:    "success data",
			payload: `{"Data":{"ToolId":"tool-1"}}`,
			dataKey: "ToolId",
			want:    "tool-1",
		},
		{
			name:    "wait failure details",
			payload: `{"Failure":{"Details":{"ResourceId":"tool-2"}}}`,
			dataKey: "ToolId",
			want:    "tool-2",
		},
		{
			name:    "failure without resource ID",
			payload: `{"Failure":{"Code":"INVALID_ARGUMENT"}}`,
			dataKey: "ToolId",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var env testutil.Envelope
			if err := json.Unmarshal([]byte(tt.payload), &env); err != nil {
				t.Fatalf("unmarshal envelope: %v", err)
			}
			if got := createdResourceID(env, tt.dataKey); got != tt.want {
				t.Fatalf("createdResourceID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCreatedDeploymentID(t *testing.T) {
	for _, tt := range []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "success data",
			payload: `{"Data":{"Deployment":{"DeploymentId":"dpl-1"}}}`,
			want:    "dpl-1",
		},
		{
			name:    "failure details",
			payload: `{"Failure":{"Details":{"ResourceId":"dpl-2"}}}`,
			want:    "dpl-2",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var env testutil.Envelope
			if err := json.Unmarshal([]byte(tt.payload), &env); err != nil {
				t.Fatalf("unmarshal envelope: %v", err)
			}
			if got := createdDeploymentID(env); got != tt.want {
				t.Fatalf("createdDeploymentID() = %q, want %q", got, tt.want)
			}
		})
	}
}

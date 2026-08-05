package pause

import (
	"testing"

	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

func TestPauseResultText(t *testing.T) {
	paused := "PAUSED"
	pausing := "PAUSING"
	failed := "PAUSE_FAILED"
	tests := []struct {
		name   string
		status *string
		want   string
	}{
		{name: "paused", status: &paused, want: "Instance paused: ins-unit"},
		{name: "pausing", status: &pausing, want: "Instance pause status: PAUSING (ins-unit)"},
		{name: "failed", status: &failed, want: "Instance pause status: PAUSE_FAILED (ins-unit)"},
		{name: "missing status", want: "Pause requested: ins-unit"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &ags.PauseSandboxInstanceResponseParams{InstanceStatus: tt.status}
			if got := pauseResultText(data, "ins-unit"); got != tt.want {
				t.Fatalf("pauseResultText() = %q, want %q", got, tt.want)
			}
		})
	}
}

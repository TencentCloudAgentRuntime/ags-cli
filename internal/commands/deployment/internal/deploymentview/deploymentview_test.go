package deploymentview

import (
	"bytes"
	"strings"
	"testing"
	"time"

	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

func TestRenderDetailsKubernetesStyle(t *testing.T) {
	deployment := fixtureDeployment()
	var out bytes.Buffer
	RenderDetails(&out, deployment)
	got := out.String()

	for _, want := range []string{
		"Name:          workspace-service",
		"ID:            dpl-a1b2c3d4",
		"Tool:          sdt-a1b2c3d4",
		"Status:        ACTIVE",
		"Tags:          env=test, owner=runtime",
		"Created:       2026-08-22T08:00:00Z",
		"Updated:       2026-08-24T08:00:00Z",
		"Scaling:\n  Min Instances:                0\n  Max Instances:                10\n  Max Requests per Instance:    100",
		"Lifecycle:\n  Idle Action:                  PAUSE\n  Idle Timeout:                 5m",
		"Affinity:\n  Mode:                         EXCLUSIVE\n  Header:                       X-Session-ID",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("details missing %q:\n%s", want, got)
		}
	}
}

func TestRenderListUsesFrozenColumnsAndKubernetesAge(t *testing.T) {
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	RenderList(&out, []*ags.Deployment{fixtureDeployment()}, 1, now)
	got := out.String()
	if !strings.Contains(got, "ID            NAME               TOOL          STATUS  SCALING   LIFECYCLE  AFFINITY   AGE") {
		t.Fatalf("unexpected table header:\n%s", got)
	}
	if !strings.Contains(got, "dpl-a1b2c3d4  workspace-service  sdt-a1b2c3d4  ACTIVE  0/10×100  PAUSE/5m   EXCLUSIVE  3d") {
		t.Fatalf("unexpected table row:\n%s", got)
	}
}

func TestAgeMatchesKubernetesHumanDurationThresholds(t *testing.T) {
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		created string
		want    string
	}{
		{"2026-08-25T07:59:15Z", "45s"},
		{"2026-08-25T07:54:30Z", "5m30s"},
		{"2026-08-25T04:30:00Z", "3h30m"},
		{"2026-08-22T08:00:00Z", "3d"},
		{"", "-"},
		{"invalid", "-"},
	} {
		if got := Age(tc.created, now); got != tc.want {
			t.Errorf("Age(%q) = %q, want %q", tc.created, got, tc.want)
		}
	}
}

func fixtureDeployment() *ags.Deployment {
	return &ags.Deployment{
		DeploymentId:   ptr("dpl-a1b2c3d4"),
		DeploymentName: ptr("workspace-service"),
		ToolId:         ptr("sdt-a1b2c3d4"),
		Status:         ptr("ACTIVE"),
		CreatedTime:    ptr("2026-08-22T08:00:00Z"),
		UpdatedTime:    ptr("2026-08-24T08:00:00Z"),
		ScalingConfiguration: &ags.ScalingConfiguration{
			MinInstanceCount:              int64ptr(0),
			MaxInstanceCount:              int64ptr(10),
			MaxInstanceRequestConcurrency: int64ptr(100),
		},
		LifecycleConfiguration: &ags.LifecycleConfiguration{
			IdleAction:         ptr("PAUSE"),
			IdleTimeoutSeconds: int64ptr(300),
		},
		AffinityConfiguration: &ags.AffinityConfiguration{
			Mode:       ptr("EXCLUSIVE"),
			HeaderName: ptr("X-Session-ID"),
		},
		Tags: []*ags.Tag{
			{Key: ptr("owner"), Value: ptr("runtime")},
			{Key: ptr("env"), Value: ptr("test")},
		},
	}
}

func ptr(value string) *string { return &value }

func int64ptr(value int64) *int64 { return &value }

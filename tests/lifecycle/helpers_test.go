package lifecycle

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"

	"github.com/TencentCloudAgentRuntime/ags-cli/tests/testutil"
)

const lifecycleCommandTimeout = 12 * time.Minute

func newCLI() *testutil.CLI {
	cli := testutil.NewCLI()
	if cli.Timeout < lifecycleCommandTimeout {
		cli.Timeout = lifecycleCommandTimeout
	}
	DeferCleanup(cli.Cleanup)
	return cli
}

func stringField(data map[string]any, key string) string {
	return testutil.StringField(data, key)
}

func createdResourceID(env testutil.Envelope, dataKey string) string {
	if id, ok := env.Data[dataKey].(string); ok && id != "" {
		return id
	}
	if env.Failure == nil {
		return ""
	}
	id, _ := env.Failure.Details["ResourceId"].(string)
	return id
}

func numberField(data map[string]any, key string) int {
	return testutil.NumberField(data, key)
}

func itemsField(data map[string]any) []any {
	return testutil.ItemsField(data)
}

type ResourceTracker struct {
	cli       *testutil.CLI
	tools     []string
	instances []string
}

func NewResourceTracker(cli *testutil.CLI) *ResourceTracker {
	tracker := &ResourceTracker{cli: cli}
	DeferCleanup(tracker.Cleanup)
	return tracker
}

func (r *ResourceTracker) AddTool(id string) {
	if id != "" {
		r.tools = append(r.tools, id)
	}
}

func (r *ResourceTracker) AddInstance(id string) {
	if id != "" {
		r.instances = append(r.instances, id)
	}
}

func (r *ResourceTracker) ForgetTool(id string) {
	r.tools = removeString(r.tools, id)
}

func (r *ResourceTracker) ForgetInstance(id string) {
	r.instances = removeString(r.instances, id)
}

func (r *ResourceTracker) Cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for i := len(r.instances) - 1; i >= 0; i-- {
		r.cli.Run(ctx, "--output", "json", "instance", "delete", r.instances[i], "--ignore-not-found")
	}
	for i := len(r.tools) - 1; i >= 0; i-- {
		r.cli.Run(ctx, "--output", "json", "tool", "delete", r.tools[i])
	}
}

func removeString(values []string, target string) []string {
	out := values[:0]
	for _, value := range values {
		if value != target {
			out = append(out, value)
		}
	}
	return out
}

func uniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

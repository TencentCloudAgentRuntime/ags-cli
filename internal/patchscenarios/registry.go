// Package patchscenarios registers reviewed, real CLI test scenarios. Add a
// scenario together with its first preview-only patch, never a success stub.
package patchscenarios

import "github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"

// Registry contains real candidate-CLI scenarios, subject to assertion review.
var Registry = patchtest.Registry{
	"session.event-count": {Assertions: []string{"event-count.consistent"}, Run: runSessionEventCount},
	"session.lifecycle":   {Assertions: sessionAssertions, Run: runSessionLifecycle},
}

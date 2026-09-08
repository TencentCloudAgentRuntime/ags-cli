// Package patchscenarios registers reviewed, real CLI test scenarios. Add a
// scenario together with its first preview-only patch, never a success stub.
package patchscenarios

import "github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"

// Registry is empty because the canonical branch has no API patches.
// Scenarios remain ordinary Go source, subject to independent assertion review.
var Registry = patchtest.Registry{}

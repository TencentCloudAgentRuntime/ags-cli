// Package patchscenarios registers reviewed, real CLI test scenarios. Add a
// scenario together with its first preview-only patch, never a success stub.
package patchscenarios

import "github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"

// Scenarios remain ordinary Go source, subject to independent assertion review.
var Registry = patchtest.Registry{
	"registry.remote.lifecycle": {Assertions: []string{"mcp.sync.changed", "mcp.sync.failed", "a2a.sync.changed", "a2a.sync.failed"}, Run: registryRemoteLifecycle},
	"registry.skill.lifecycle":  {Assertions: []string{"skill.package.roundtrip", "skill.inline.readback"}, Run: registrySkillLifecycle},
	"registry.custom.lifecycle": {
		Assertions: []string{"registry.description", "registry.tags.readback", "record.label.readback", "version.decisions", "record.history", "manual.remote.rejected", "lists.filtered", "input.boundaries", "labels.removed", "mcp.manual.readback", "a2a.manual.readback", "version.deleted"},
		Run:        registryCustomLifecycle,
	},
}

// The complete contract scenario deliberately requires every lifecycle, so a
// missing remote fixture or failed cleanup cannot produce a passing report.
func init() {
	ids := []string{"registry.custom.lifecycle", "registry.skill.lifecycle", "registry.remote.lifecycle"}
	var assertions []string
	for _, id := range ids {
		assertions = append(assertions, Registry[id].Assertions...)
	}
	Registry["registry.lifecycle"] = patchtest.Scenario{Assertions: assertions, Run: func(s *patchtest.Session) error {
		if err := registryFixtureConfigured(); err != nil {
			return err
		}
		for _, id := range ids {
			if err := Registry[id].Run(s); err != nil {
				return err
			}
		}
		return nil
	}}
}

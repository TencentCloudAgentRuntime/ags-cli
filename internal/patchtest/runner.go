// Package patchtest provides fail-closed execution for registered live CLI
// scenarios. Assertion records are execution evidence, not proof of test quality.
package patchtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"slices"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchcoverage"
)

type Scenario struct {
	Assertions []string
	Run        func(*Session) error
}
type Registry map[string]Scenario

func (r Registry) Coverage() patchcoverage.Registry {
	out := patchcoverage.Registry{}
	for id, s := range r {
		if s.Run != nil {
			out[id] = s.Assertions
		}
	}
	return out
}

type Result struct {
	Scenario   string   `json:"scenario"`
	Status     string   `json:"status"`
	Assertions []string `json:"assertions"`
	Calls      int      `json:"cli_calls"`
	Cleanup    string   `json:"cleanup"`
}

// Session is single-threaded. Register cleanup immediately after obtaining any
// resource ID, including IDs returned with partial failures.
type Session struct {
	Context     context.Context
	binary      string
	env         []string
	assertions  map[string]bool
	allowed     []string
	failed      bool
	skipped     bool
	calls       int
	cleanups    []func(context.Context) error
	noResources bool
}

// CLI invokes the candidate binary, never the SDK or a configurable executable.
// Output stays in memory for assertions and is not copied into public reports.
func (s *Session) CLI(ctx context.Context, args ...string) ([]byte, error) {
	s.calls++
	cmd := exec.CommandContext(ctx, s.binary, args...)
	cmd.Env = s.env
	return cmd.Output()
}

// Assert must be called after checking the actual request/readback/behavior.
func (s *Session) Assert(id string, condition bool) error {
	if !condition || !slices.Contains(s.allowed, id) {
		s.failed = true
		return fmt.Errorf("assertion failed")
	}
	s.assertions[id] = true
	return nil
}

func (s *Session) Skip()                                  { s.skipped = true }
func (s *Session) Cleanup(fn func(context.Context) error) { s.cleanups = append(s.cleanups, fn) }

// NoResources is an explicit, reviewer-audited declaration for read-only cases.
// It does not waive any registered cleanup callbacks.
func (s *Session) NoResources() { s.noResources = true }

func Run(ctx context.Context, plan patchcoverage.Plan, registry Registry, binary string, env []string) ([]Result, error) {
	selected := map[string]map[string]bool{}
	for _, b := range plan.Bindings {
		if b.Scenario == "" {
			continue
		}
		if selected[b.Scenario] == nil {
			selected[b.Scenario] = map[string]bool{}
		}
		for _, a := range b.Assertions {
			selected[b.Scenario][a] = true
		}
	}
	var results []Result
	var failures []error
	for _, id := range slices.Sorted(maps.Keys(selected)) {
		spec, ok := registry[id]
		if !ok || spec.Run == nil {
			return results, fmt.Errorf("unregistered scenario %s", id)
		}
		s := &Session{Context: ctx, binary: binary, env: env, allowed: spec.Assertions, assertions: map[string]bool{}}
		result := Result{Scenario: id, Status: "pass", Cleanup: "pass"}
		err := invoke(func() error { return spec.Run(s) })
		if err != nil || s.failed || ctx.Err() != nil {
			result.Status = "fail"
		}
		if s.calls == 0 {
			result.Status = "fail"
		}
		for a := range selected[id] {
			if !s.assertions[a] {
				result.Status = "fail"
			}
		}
		if len(s.cleanups) == 0 && !s.noResources {
			result.Cleanup = "unconfirmed"
			result.Status = "fail"
		}
		for i := len(s.cleanups) - 1; i >= 0; i-- {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			err := invoke(func() error { return s.cleanups[i](cleanupCtx) })
			cleanupTimedOut := cleanupCtx.Err() != nil
			cancel()
			if err != nil || cleanupTimedOut {
				result.Cleanup = "fail"
				result.Status = "fail"
			}
		}
		result.Assertions = slices.Sorted(maps.Keys(s.assertions))
		result.Calls = s.calls
		// Preserve skip accounting even when the skipped body did not execute
		// its assertions. Cleanup status remains separately visible.
		if s.skipped {
			result.Status = "skip"
		}
		results = append(results, result)
		if result.Status != "pass" {
			failures = append(failures, fmt.Errorf("scenario %s did not pass", id))
		}
	}
	return results, errors.Join(failures...)
}

func invoke(fn func() error) (err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("scenario or cleanup panicked")
		}
	}()
	return fn()
}

// EncodeReport deliberately excludes raw scenario errors, argv and responses.
func EncodeReport(value any) ([]byte, error) { return json.MarshalIndent(value, "", "  ") }

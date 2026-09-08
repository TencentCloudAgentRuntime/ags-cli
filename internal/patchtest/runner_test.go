package patchtest

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchcoverage"
)

// The local subprocess tests the runner protocol only, never live correctness.
func TestChild(t *testing.T) {
	if os.Getenv("PATCH_RUNNER_CHILD") == "1" {
		os.Exit(0)
	}
}

func TestStrictRunner(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	plan := patchcoverage.Plan{Bindings: []patchcoverage.Binding{{Scenario: "case", Assertions: []string{"readback"}}}}
	for _, mode := range []string{"pass", "skip", "missing-assertion", "failed-assertion", "unknown-assertion", "failure", "panic", "cleanup-failure", "cleanup-panic", "unconfirmed", "no-cli"} {
		t.Run(mode, func(t *testing.T) {
			cleaned := false
			registry := Registry{"case": {Assertions: []string{"readback"}, Run: func(s *Session) error {
				if mode != "unconfirmed" {
					s.Cleanup(func(context.Context) error {
						cleaned = true
						if mode == "cleanup-panic" {
							panic("secret")
						}
						if mode == "cleanup-failure" {
							return errors.New("secret")
						}
						return nil
					})
				}
				if mode == "panic" {
					panic("secret")
				}
				if mode != "no-cli" {
					if _, err := s.CLI(s.Context, "-test.run=TestChild"); err != nil {
						return err
					}
				}
				if mode == "skip" {
					s.Skip()
				}
				if mode == "failed-assertion" {
					_ = s.Assert("readback", false)
				}
				if mode == "unknown-assertion" {
					_ = s.Assert("unknown", true)
				}
				if mode != "missing-assertion" {
					_ = s.Assert("readback", true)
				}
				if mode == "failure" {
					return errors.New("secret")
				}
				return nil
			}}}
			results, err := Run(t.Context(), plan, registry, binary, append(os.Environ(), "PATCH_RUNNER_CHILD=1"))
			if (err == nil) != (mode == "pass") {
				t.Fatalf("%s: %+v %v", mode, results, err)
			}
			if mode != "unconfirmed" && !cleaned {
				t.Fatal("cleanup not attempted")
			}
		})
	}
	if _, err := Run(t.Context(), plan, Registry{}, binary, nil); err == nil {
		t.Fatal("unknown selected scenario passed")
	}
}

func TestCleanupAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	var order []int
	registry := Registry{"case": {Assertions: []string{"a"}, Run: func(s *Session) error {
		s.Cleanup(func(ctx context.Context) error {
			if ctx.Err() != nil {
				t.Error("cleanup inherited cancelled context")
			}
			order = append(order, 1)
			return nil
		})
		s.Cleanup(func(context.Context) error { order = append(order, 2); return errors.New("cleanup failure") })
		cancel()
		return nil
	}}}
	plan := patchcoverage.Plan{Bindings: []patchcoverage.Binding{{Scenario: "case", Assertions: []string{"a"}}}}
	results, err := Run(ctx, plan, registry, "unused", nil)
	if err == nil || len(order) != 2 || order[0] != 2 || order[1] != 1 || results[0].Cleanup != "fail" {
		t.Fatal(order, results, err)
	}
}

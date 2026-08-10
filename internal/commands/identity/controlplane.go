// Package identity implements the top-level "agr identity" command group.
//
// Architecture note (migration-friendly adapter pattern):
//
// These commands are implemented as pure "workflow" modules that call Cloud API
// Actions via ControlPlane.Call(action, request). When the backend API schema
// stabilizes and is added to api.json + mapping.yaml:
//
//  1. Run `go run ./cmd/internal/cobragen` to generate api.generated.go
//  2. Change Source from command.SourceWorkflow to command.SourceMixedAPI
//  3. Reference APIDescriptor() for flags (keeps custom validation/rendering)
//
// The command IDs, paths, and flag names are chosen to match the expected
// Cloud API field names, ensuring a seamless transition.
package identity

import "context"

// ControlPlane is the minimal interface required by identity commands.
// It matches the signature used by apicli.Executor, so migration to
// mixed-api mode requires zero interface changes.
type ControlPlane interface {
	Call(ctx context.Context, action string, request map[string]any) (any, error)
}

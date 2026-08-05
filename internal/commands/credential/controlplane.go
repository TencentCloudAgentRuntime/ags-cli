// Package credential implements the top-level "agr credential" command group.
// See identity/controlplane.go for the migration-friendly adapter pattern notes.
package credential

import "context"

// ControlPlane is the minimal interface required by credential commands.
type ControlPlane interface {
	Call(ctx context.Context, action string, request map[string]any) (any, error)
}

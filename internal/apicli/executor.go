package apicli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
)

// ControlPlane is the minimal dependency required to execute a generated API
// command. The concrete implementation adapts map-based requests to the typed
// TencentCloud SDK.
type ControlPlane interface {
	Call(ctx context.Context, action string, request map[string]any) (any, error)
}

// Executor sends generated API requests through the configured control plane.
// It is intentionally small because request assembly and rendering live in
// separate layers.
type Executor struct {
	desc         APIDescriptor
	controlPlane ControlPlane
}

// NewExecutor creates an executor for desc using controlPlane when it implements
// ControlPlane. A nil control plane is reported at execution time so modules can
// still be described without runtime dependencies.
func NewExecutor(desc APIDescriptor, controlPlane any) *Executor {
	cp, _ := controlPlane.(ControlPlane)
	return &Executor{desc: desc, controlPlane: cp}
}

// Execute sends request to the descriptor's API action and wraps the response.
func (e *Executor) Execute(ctx context.Context, request map[string]any) (*command.Result, error) {
	if e.controlPlane == nil {
		return nil, fmt.Errorf("apicli executor for %s requires command.Deps.ControlPlane implementing apicli.ControlPlane", e.desc.Spec.ID)
	}
	response, err := e.controlPlane.Call(ctx, e.desc.API.Action, request)
	if err != nil {
		return nil, err
	}
	return &command.Result{
		Data: response,
		MetaExtra: map[string]any{
			"Action":       e.desc.API.Action,
			"RequestType":  e.desc.API.RequestType,
			"ResponseType": e.desc.API.ResponseType,
		},
	}, nil
}

// NewModule converts a generated API descriptor into a command registry module.
// The module keeps descriptor metadata available for schema/help generation and
// builds the parser/executor pipeline only when invoked.
func NewModule(desc APIDescriptor) command.Module {
	spec := desc.CommandSpec()
	desc.Spec = spec
	return command.Module{
		Descriptor: command.Descriptor{
			Spec:   spec,
			Groups: desc.Groups,
			API:    desc,
			Source: command.SourceAPICli,
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			builder := NewRequestBuilder(desc)
			executor := NewExecutor(desc, deps.ControlPlane)
			return command.Runtime{
				Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
					apiReq, err := builder.Build(req)
					if err != nil {
						return nil, err
					}
					return executor.Execute(ctx, apiReq)
				}),
				Renderer: command.RendererFunc(func(ctx context.Context, result *command.Result) error {
					if result.Text != nil {
						result.Text(deps.IO.Out)
						return nil
					}
					return renderJSON(deps.IO.Out, result.Data)
				}),
			}, nil
		},
	}
}

func renderJSON(w io.Writer, data any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

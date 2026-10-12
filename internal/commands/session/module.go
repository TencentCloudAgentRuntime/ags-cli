// Package session owns the presentation of session spaces, conversations, and events.
package session

import (
	"context"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
)

// NewModule adds session presentation to a generated API module.
func NewModule(api apicli.APIDescriptor) command.Module {
	module := apicli.NewModule(api)
	generated := module.Descriptor
	module.Descriptor.Generated = &generated
	module.Descriptor.Source = command.SourceMixedAPI
	module.Descriptor.Spec.RequiresAuth = true
	build := module.Build
	module.Build = func(deps command.Deps) (command.Runtime, error) {
		runtime, err := build(deps)
		if err != nil {
			return runtime, err
		}
		handler := runtime.Handler
		runtime.Handler = command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
			result, err := handler.Run(ctx, req)
			if err != nil || result == nil {
				return result, err
			}
			if result.Text == nil {
				result.Text, err = sessionTextRenderer(api.Spec.ID, result.Data)
			}
			return result, err
		})
		return runtime, nil
	}
	return module
}

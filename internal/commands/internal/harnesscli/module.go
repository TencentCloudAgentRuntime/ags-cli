//go:build preview

package harnesscli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// GroupsOf returns the leading groups of a generated descriptor, so workflows
// register metadata identical to the generated commands under the same parent.
func GroupsOf(api apicli.APIDescriptor, depth int) []command.GroupSpec {
	return append([]command.GroupSpec(nil), api.Groups[:depth]...)
}

// Handler is a wrapper body with its builder, executor and API client ready.
type Handler func(ctx context.Context, req command.Request, env Env) (*command.Result, error)

// Env bundles per-invocation dependencies for wrapper handlers.
type Env struct {
	Deps     command.Deps
	Builder  *apicli.RequestBuilder
	Executor *apicli.Executor
	Client   Client
	API      apicli.APIDescriptor
}

// Mixed builds a mixed-api module: the generated descriptor stays available for
// schema checks while the final Spec carries workflow flags and overrides.
func Mixed(api apicli.APIDescriptor, customize func(*command.Spec), handler Handler) command.Module {
	generated := api.CommandSpec()
	spec := api.CommandSpec()
	if customize != nil {
		customize(&spec)
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec:      spec,
			Generated: &command.Descriptor{Spec: generated, Groups: api.Groups, API: api, Source: command.SourceAPICli},
			Groups:    api.Groups,
			API:       api,
			Source:    command.SourceMixedAPI,
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			client, err := NewClient(deps, api.Spec.ID)
			if err != nil {
				return command.Runtime{}, err
			}
			env := Env{Deps: deps, Builder: apicli.NewRequestBuilder(api), Executor: apicli.NewExecutor(api, deps.ControlPlane), Client: client, API: api}
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				return handler(ctx, req, env)
			})}, nil
		},
	}
}

// WithAPI returns an Env that executes another generated Action with the same
// dependencies, for wrappers that chain two Actions.
func (e Env) WithAPI(api apicli.APIDescriptor) Env {
	e.API = api
	e.Builder = apicli.NewRequestBuilder(api)
	e.Executor = apicli.NewExecutor(api, e.Deps.ControlPlane)
	return e
}

// Execute sends the request and decodes the response object.
func (e Env) Execute(ctx context.Context, request map[string]any) (*command.Result, apivalue.Object, error) {
	result, err := e.Executor.Execute(ctx, request)
	if err != nil {
		return nil, nil, err
	}
	response, err := apivalue.Decode(result.Data)
	if err != nil {
		return nil, nil, err
	}
	return result, response, nil
}

// WithCreated keeps the identifiers of a resource that was created before a
// later step failed, so callers can clean up or retry without guessing.
func WithCreated(err error, ids map[string]any, prefix, fix string) error {
	classified := output.ClassifyError(err)
	failure := *classified.Failure
	details := map[string]any{"Created": true}
	for key, value := range failure.Details {
		details[key] = value
	}
	for key, value := range ids {
		details[key] = value
	}
	failure.Details = details
	failure.Message = prefix + ": " + failure.Message
	if fix != "" {
		failure.Fix = fix
	}
	return (&output.CLIError{Failure: &failure, ExitCode: classified.ExitCode}).WithCause(err)
}

// DryRun returns the request that would be sent, without sending it.
func (e Env) DryRun(request map[string]any) *command.Result {
	return &command.Result{
		Data: map[string]any{"DryRun": true, "Action": e.API.API.Action, "Request": request},
		Text: func(w io.Writer) {
			fmt.Fprintf(w, "Dry run: %s would be sent with:\n", e.API.API.Action)
			encoded, _ := json.MarshalIndent(request, "", "  ")
			fmt.Fprintln(w, string(encoded))
			fmt.Fprintln(w, "\nNo changes were made.")
		},
		MetaExtra: map[string]any{"Action": e.API.API.Action, "DryRun": true},
	}
}

// Next writes a follow-up command hint to stderr in text mode only, keeping
// stdout clean for scripts.
func (e Env) Next(format string, args ...any) {
	fmt.Fprintf(e.Deps.IO.ErrOut, "Next: "+format+"\n", args...)
}

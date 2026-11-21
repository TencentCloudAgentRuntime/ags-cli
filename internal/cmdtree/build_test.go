package cmdtree

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	toollist "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/tool/list"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/iostreams"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type fakeControlPlane struct {
	action  string
	request map[string]any
}

func (f *fakeControlPlane) Call(_ context.Context, action string, request map[string]any) (any, error) {
	f.action = action
	f.request = request
	return map[string]any{"Action": action, "Request": request}, nil
}

func TestBuildExecutesGeneratedAPIModule(t *testing.T) {
	registry := command.NewRegistry()
	if err := registry.Register(toollist.GeneratedModule()); err != nil {
		t.Fatalf("register module: %v", err)
	}
	ios, _, stdout, _ := iostreams.Test()
	cp := &fakeControlPlane{}
	root, err := Build(registry, command.Deps{IO: ios, ControlPlane: cp}, Options{RootUse: "agr"})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	root.SetArgs([]string{"tool", "list", "--limit", "7"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if cp.action != "DescribeSandboxToolList" {
		t.Fatalf("action = %q", cp.action)
	}
	if cp.request["Limit"] != 7 {
		t.Fatalf("Limit = %#v, want 7", cp.request["Limit"])
	}
	var rendered map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &rendered); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if rendered["Action"] != "DescribeSandboxToolList" {
		t.Fatalf("rendered Action = %#v", rendered["Action"])
	}
}

func TestBuildRejectsMissingGroupMetadata(t *testing.T) {
	registry := command.NewRegistry()
	if err := registry.Register(simpleModule("x.y", []string{"x", "y"}, nil)); err != nil {
		t.Fatalf("register module: %v", err)
	}
	_, err := Build(registry, command.Deps{}, Options{RootUse: "agr"})
	if err == nil || !strings.Contains(err.Error(), "missing group metadata") {
		t.Fatalf("expected missing group metadata error, got %v", err)
	}
}

func TestBuildRejectsDuplicateSiblingAlias(t *testing.T) {
	group := command.GroupSpec{Path: []string{"x"}, Use: "x"}
	registry := command.NewRegistry()
	if err := registry.Register(simpleModule("x.one", []string{"x", "one"}, []command.GroupSpec{group})); err != nil {
		t.Fatalf("register first: %v", err)
	}
	second := simpleModule("x.two", []string{"x", "two"}, []command.GroupSpec{group})
	second.Descriptor.Spec.Aliases = []string{"one"}
	if err := registry.Register(second); err != nil {
		t.Fatalf("register second: %v", err)
	}
	_, err := Build(registry, command.Deps{}, Options{RootUse: "agr"})
	if err == nil || !strings.Contains(err.Error(), "duplicate command alias/name") {
		t.Fatalf("expected duplicate alias error, got %v", err)
	}
}

func TestBuildRejectsDuplicateShorthand(t *testing.T) {
	registry := command.NewRegistry()
	module := simpleModule("x", []string{"x"}, nil)
	module.Descriptor.Spec.Flags = []command.FlagSpec{
		{Name: "first", Shorthand: "f", Type: command.FlagString},
		{Name: "force", Shorthand: "f", Type: command.FlagBool},
	}
	if err := registry.Register(module); err != nil {
		t.Fatalf("register module: %v", err)
	}
	_, err := Build(registry, command.Deps{}, Options{RootUse: "agr"})
	if err == nil || !strings.Contains(err.Error(), "duplicate shorthand") {
		t.Fatalf("expected duplicate shorthand error, got %v", err)
	}
}

func TestBuildRejectsWorkflowGeneratedFlagConflict(t *testing.T) {
	registry := command.NewRegistry()
	module := simpleModule("x", []string{"x"}, nil)
	module.Descriptor.Spec.Flags = []command.FlagSpec{
		{Name: "request", Type: command.FlagString, Generated: true},
		{Name: "request", Type: command.FlagBool, Workflow: true},
	}
	if err := registry.Register(module); err != nil {
		t.Fatalf("register module: %v", err)
	}
	_, err := Build(registry, command.Deps{}, Options{RootUse: "agr"})
	if err == nil || !strings.Contains(err.Error(), "workflow flag") {
		t.Fatalf("expected workflow flag conflict error, got %v", err)
	}
}

func TestBuildRejectsMixedModuleChangedID(t *testing.T) {
	module := mixedAPIModule()
	module.Descriptor.Spec.ID = "instance.remove"
	err := buildSingle(module)
	if err == nil || !strings.Contains(err.Error(), "changed generated command id") {
		t.Fatalf("expected changed id error, got %v", err)
	}
}

func TestBuildRejectsMixedModuleChangedPath(t *testing.T) {
	module := mixedAPIModule()
	module.Descriptor.Spec.Path = []string{"instance", "remove"}
	err := buildSingle(module)
	if err == nil || !strings.Contains(err.Error(), "changed generated path") {
		t.Fatalf("expected changed path error, got %v", err)
	}
}

func TestBuildRejectsMixedModuleChangedAPIAction(t *testing.T) {
	module := mixedAPIModule()
	api := module.Descriptor.API.(apicli.APIDescriptor)
	api.API.Action = "DeleteSandboxInstance"
	module.Descriptor.API = api
	err := buildSingle(module)
	if err == nil || !strings.Contains(err.Error(), "changed generated API action") {
		t.Fatalf("expected changed API action error, got %v", err)
	}
}

func TestBuildRejectsMixedModuleMissingGeneratedSnapshot(t *testing.T) {
	module := mixedAPIModule()
	module.Descriptor.Generated = nil
	err := buildSingle(module)
	if err == nil || !strings.Contains(err.Error(), "missing generated descriptor snapshot") {
		t.Fatalf("expected missing generated snapshot error, got %v", err)
	}
}

func TestBuildRejectsMixedWorkflowFlagGeneratedAliasConflict(t *testing.T) {
	module := mixedAPIModule()
	module.Descriptor.Spec.Flags = append(module.Descriptor.Spec.Flags, command.FlagSpec{
		Name:     "workflow-id",
		Aliases:  []string{"id"},
		Type:     command.FlagString,
		Workflow: true,
	})
	err := buildSingle(module)
	if err == nil || !strings.Contains(err.Error(), "workflow flag") {
		t.Fatalf("expected workflow/generated flag conflict, got %v", err)
	}
}

func TestBuildRejectsMixedWorkflowFlagGeneratedShorthandConflict(t *testing.T) {
	module := mixedAPIModule()
	module.Descriptor.Spec.Flags = append(module.Descriptor.Spec.Flags, command.FlagSpec{
		Name:      "generated-short",
		Shorthand: "x",
		Type:      command.FlagString,
		Generated: true,
	})
	module.Descriptor.Spec.Flags = append(module.Descriptor.Spec.Flags, command.FlagSpec{
		Name:      "workflow-short",
		Shorthand: "x",
		Type:      command.FlagString,
		Workflow:  true,
	})
	err := buildSingle(module)
	if err == nil || !strings.Contains(err.Error(), "workflow flag") {
		t.Fatalf("expected workflow/generated shorthand conflict, got %v", err)
	}
}

func TestBuildRejectsMixedModuleMissingOutputDataType(t *testing.T) {
	module := mixedAPIModule()
	module.Descriptor.Spec.Output.DataType = ""
	err := buildSingle(module)
	if err == nil || !strings.Contains(err.Error(), "must declare output data type") {
		t.Fatalf("expected output data type error, got %v", err)
	}
}

func TestBuildRejectsNonDefaultAPIModuleMissingOutputDataType(t *testing.T) {
	module := mixedAPIModule()
	module.Descriptor.Source = command.SourceWorkflow
	module.Descriptor.Spec.Output.DataType = ""
	err := buildSingle(module)
	if err == nil || !strings.Contains(err.Error(), "must declare output data type") {
		t.Fatalf("expected output data type error, got %v", err)
	}
}

func TestBuildRequestCapturesDashPosition(t *testing.T) {
	spec := command.Spec{
		ID: "instance.exec",
		Args: []command.ArgSpec{
			{Name: "args", Repeatable: true},
		},
		Flags: []command.FlagSpec{
			{Name: "env", Type: command.FlagStringArray},
		},
	}
	var req command.Request
	cmd := &cobra.Command{
		Use:  "exec",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			req, err = BuildRequest(cmd, spec, args, command.Deps{})
			return err
		},
	}
	if err := registerFlags(cmd.Flags(), spec.Flags); err != nil {
		t.Fatalf("register flags: %v", err)
	}
	cmd.SetArgs([]string{"ins-1", "--env", "A=B", "--", "echo", "hello"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if req.DashPos != 1 {
		t.Fatalf("DashPos = %d, want 1", req.DashPos)
	}
	if strings.Join(req.RawArgs, " ") != "ins-1 echo hello" {
		t.Fatalf("RawArgs = %#v", req.RawArgs)
	}
}

func TestBuildAcceptsValidMixedModule(t *testing.T) {
	if err := buildSingle(mixedAPIModule()); err != nil {
		t.Fatalf("valid mixed module rejected: %v", err)
	}
}

func simpleModule(id string, path []string, groups []command.GroupSpec) command.Module {
	return command.Module{
		Descriptor: command.Descriptor{
			Spec:   command.Spec{ID: id, Path: path, Aliases: []string{"alias-" + id}},
			Groups: groups,
		},
		Build: func(command.Deps) (command.Runtime, error) {
			return command.Runtime{
				Handler: command.HandlerFunc(func(context.Context, command.Request) (*command.Result, error) {
					return &command.Result{Data: map[string]any{"ok": true}}, nil
				}),
				Renderer: command.RendererFunc(func(context.Context, *command.Result) error { return nil }),
			}, nil
		},
	}
}

func mixedAPIModule() command.Module {
	api := apicli.APIDescriptor{
		Spec: command.Spec{
			ID:           "instance.delete",
			Path:         []string{"instance", "delete"},
			Use:          "delete <instance-id>",
			SupportsJSON: true,
			Args: []command.ArgSpec{
				{Name: "instance-id", Required: true},
			},
			Output: command.OutputSpec{DataType: "StopSandboxInstanceResponse"},
		},
		Groups: []command.GroupSpec{
			{Path: []string{"instance"}, Use: "instance", Short: "Manage instances"},
		},
		API: apicli.APISpec{
			Action:       "StopSandboxInstance",
			RequestType:  "StopSandboxInstanceRequest",
			ResponseType: "StopSandboxInstanceResponse",
		},
		Fields: []apicli.FieldSpec{
			{
				Name: "InstanceId",
				Inputs: []apicli.InputSpec{
					{Name: "instance-id", Positional: true},
					{Name: "id", Flag: "id", Type: command.FlagString, Shorthand: "i"},
				},
			},
		},
	}
	generatedSpec := api.CommandSpec()
	finalSpec := generatedSpec
	finalSpec.Flags = append(finalSpec.Flags, command.FlagSpec{
		Name:     "ignore-not-found",
		Type:     command.FlagBool,
		Workflow: true,
	})
	finalSpec.Output = command.OutputSpec{DataType: "DeleteData"}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: finalSpec,
			Generated: &command.Descriptor{
				Spec:   generatedSpec,
				Groups: api.Groups,
				API:    api,
				Source: command.SourceAPICli,
			},
			Groups: api.Groups,
			API:    api,
			Source: command.SourceMixedAPI,
		},
		Build: func(command.Deps) (command.Runtime, error) {
			return command.Runtime{
				Handler: command.HandlerFunc(func(context.Context, command.Request) (*command.Result, error) {
					return &command.Result{}, nil
				}),
			}, nil
		},
	}
}

func buildSingle(module command.Module) error {
	registry := command.NewRegistry()
	if err := registry.Register(module); err != nil {
		return err
	}
	_, err := Build(registry, command.Deps{}, Options{RootUse: "agr"})
	return err
}

var _ apicli.ControlPlane = (*fakeControlPlane)(nil)

func TestBuildModuleCommand_FlagUsageIncludesStructuredHelp(t *testing.T) {
	module := command.Module{
		Descriptor: command.Descriptor{
			Spec: command.Spec{
				ID:   "test.cmd",
				Path: []string{"test", "cmd"},
				Flags: []command.FlagSpec{
					{
						Name:     "filters",
						Type:     command.FlagString,
						Usage:    "Filter results",
						Format:   `[{"Name":"<field>","Values":["<value>"]}]`,
						Fields:   []string{"Name: filter field", "Values: accepted values for the field"},
						Values:   []string{"Status: RUNNING, STOPPED"},
						Examples: []string{`agr test cmd --filters '[{"Name":"Status","Values":["RUNNING"]}]'`},
					},
					{Name: "offset", Type: command.FlagInt, Usage: "Offset"},
				},
			},
			Groups: []command.GroupSpec{{Path: []string{"test"}, Use: "test", Short: "Test"}},
		},
		Build: func(command.Deps) (command.Runtime, error) {
			return command.Runtime{
				Handler: command.HandlerFunc(func(context.Context, command.Request) (*command.Result, error) {
					return &command.Result{}, nil
				}),
			}, nil
		},
	}

	cmd, err := BuildModuleCommand(module, command.Deps{})
	if err != nil {
		t.Fatalf("BuildModuleCommand: %v", err)
	}

	f := cmd.Flags().Lookup("filters")
	if f == nil {
		t.Fatal("--filters flag not found")
	}
	for _, want := range []string{
		"Filter results",
		"Format:",
		`[{"Name":"<field>","Values":["<value>"]}]`,
		"Fields:",
		"Name: filter field",
		"Values: accepted values for the field",
		"Values:",
		"Status: RUNNING, STOPPED",
		"Examples:",
		`agr test cmd --filters '[{"Name":"Status","Values":["RUNNING"]}]'`,
	} {
		if !strings.Contains(f.Usage, want) {
			t.Fatalf("--filters usage missing %q:\n%s", want, f.Usage)
		}
	}

	f2 := cmd.Flags().Lookup("offset")
	if f2 == nil {
		t.Fatal("--offset flag not found")
	}
	if f2.Usage != "Offset" {
		t.Fatalf("--offset usage = %q, want Offset", f2.Usage)
	}
}

func TestBuildModuleCommand_PreservesDeclaredFlagOrderWhenRequested(t *testing.T) {
	module := command.Module{
		Descriptor: command.Descriptor{Spec: command.Spec{
			ID:                "test.ordered",
			Path:              []string{"test", "ordered"},
			PreserveFlagOrder: true,
			Flags: []command.FlagSpec{
				{Name: "primary", Type: command.FlagString},
				{Name: "zeta", Type: command.FlagString},
				{Name: "alpha", Type: command.FlagString},
			},
		}},
		Build: func(command.Deps) (command.Runtime, error) {
			return command.Runtime{Handler: command.HandlerFunc(func(context.Context, command.Request) (*command.Result, error) {
				return &command.Result{}, nil
			})}, nil
		},
	}

	cmd, err := BuildModuleCommand(module, command.Deps{})
	if err != nil {
		t.Fatalf("BuildModuleCommand: %v", err)
	}
	cmd.InitDefaultHelpFlag()
	var got []string
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		got = append(got, flag.Name)
	})
	want := []string{"primary", "zeta", "alpha", "help"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("flag order = %v, want %v", got, want)
	}
}

func TestBuildModuleCommand_FlagUsagePreservesExistingAnnotations(t *testing.T) {
	module := command.Module{
		Descriptor: command.Descriptor{
			Spec: command.Spec{
				ID:   "test.ann",
				Path: []string{"test", "ann"},
				Flags: []command.FlagSpec{
					{
						Name:        "config",
						Type:        command.FlagString,
						Usage:       "Config file",
						Format:      "@file or -",
						Annotations: map[string][]string{"custom.key": {"custom-value"}},
					},
				},
			},
			Groups: []command.GroupSpec{{Path: []string{"test"}, Use: "test", Short: "Test"}},
		},
		Build: func(command.Deps) (command.Runtime, error) {
			return command.Runtime{
				Handler: command.HandlerFunc(func(context.Context, command.Request) (*command.Result, error) {
					return &command.Result{}, nil
				}),
			}, nil
		},
	}

	cmd, err := BuildModuleCommand(module, command.Deps{})
	if err != nil {
		t.Fatalf("BuildModuleCommand: %v", err)
	}

	f := cmd.Flags().Lookup("config")
	if f == nil {
		t.Fatal("--config flag not found")
	}
	if ann, ok := f.Annotations["custom.key"]; !ok || ann[0] != "custom-value" {
		t.Fatalf("missing or wrong custom.key: %v", f.Annotations)
	}
	if !strings.Contains(f.Usage, "Format:\n  @file or -") {
		t.Fatalf("--config usage missing format:\n%s", f.Usage)
	}
}

func TestFlagAliasDocumentationAndConflicts(t *testing.T) {
	spec := command.FlagSpec{
		Name:                 "filters",
		Aliases:              []string{"filter"},
		Type:                 command.FlagString,
		HideAliases:          true,
		RejectAliasConflicts: true,
	}
	for _, tc := range []struct {
		name    string
		args    []string
		want    string
		wantErr string
	}{
		{name: "canonical", args: []string{"--filters", "a"}, want: "a"},
		{name: "alias", args: []string{"--filter", "a"}, want: "a"},
		{name: "both", args: []string{"--filters", "a", "--filter", "b"}, wantErr: "cannot combine --filters with --filter"},
		{name: "both reversed", args: []string{"--filter", "b", "--filters", "a"}, wantErr: "cannot combine --filters with --filter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			if err := registerFlags(flags, []command.FlagSpec{spec}); err != nil {
				t.Fatalf("registerFlags: %v", err)
			}
			if alias := flags.Lookup("filter"); alias == nil || !alias.Hidden {
				t.Fatalf("alias flag = %+v, want a hidden --filter", alias)
			}
			if tc.wantErr == "" && flags.Lookup("filters") == nil {
				t.Fatal("--filters flag not found")
			}
			if err := flags.Parse(tc.args); err != nil {
				t.Fatalf("Parse: %v", err)
			}
			value, err := readFlagValue(flags, spec)
			if tc.wantErr != "" {
				var cliErr *output.CLIError
				if !errors.As(err, &cliErr) {
					t.Fatalf("error = %v, want a CLI usage error", err)
				}
				if cliErr.Failure.Code != "CONFLICTING_FLAGS" || cliErr.Failure.Kind != output.KindUsage || cliErr.ExitCode != output.ExitUsage {
					t.Fatalf("failure = %+v, exit %d", cliErr.Failure, cliErr.ExitCode)
				}
				if cliErr.Failure.Message != tc.wantErr {
					t.Fatalf("message = %q, want %q", cliErr.Failure.Message, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("readFlagValue: %v", err)
			}
			if !value.Changed || value.String != tc.want {
				t.Fatalf("value = %+v, want %q", value, tc.want)
			}
		})
	}
}

func TestFlagAliasesKeepFirstSpellingWithoutConflictCheck(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	spec := command.FlagSpec{Name: "filters", Aliases: []string{"filter"}, Type: command.FlagString}
	if err := registerFlags(flags, []command.FlagSpec{spec}); err != nil {
		t.Fatalf("registerFlags: %v", err)
	}
	if flags.Lookup("filter").Hidden {
		t.Fatal("an alias without HideAliases must stay visible")
	}
	if err := flags.Parse([]string{"--filters", "a", "--filter", "b"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	value, err := readFlagValue(flags, spec)
	if err != nil {
		t.Fatalf("readFlagValue: %v", err)
	}
	if !value.Changed || value.String != "a" {
		t.Fatalf("value = %+v, want the canonical spelling to win", value)
	}
}

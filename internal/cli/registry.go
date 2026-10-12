package cli

import (
	"fmt"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cmdtree"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/controlplane"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/spf13/cobra"
)

// AttachCommandRegistry attaches generated and workflow command modules to the
// process root command and registers their descriptors for schema lookup.
func AttachCommandRegistry(registry *command.Registry) error {
	registerRegistrySchemaDescriptors(registry.Descriptors())
	return cmdtree.AddTo(rootCmd, registry, command.Deps{}, cmdtree.Options{
		ModuleCommandBuilder: buildCLIRegistryCommand,
	})
}

func buildCLIRegistryCommand(module command.Module, _ command.Deps) (*cobra.Command, error) {
	cmd, err := cmdtree.BuildModuleCommand(module, command.Deps{})
	if err != nil {
		return nil, err
	}
	spec := module.Descriptor.Spec
	baseArgs := cmd.Args
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		if generateSkeleton && supportsSkeleton(spec.ID) {
			return nil
		}
		if requestFlagChanged(cmd) {
			return nil
		}
		if baseArgs != nil {
			return baseArgs(cmd, args)
		}
		return nil
	}
	if !spec.SupportsJSON && !spec.SupportsNDJSON {
		cmd.RunE = WrapNoJSON(func(cmd *cobra.Command, args []string) error {
			result, err := runRegistryModule(cmd, module, spec, args)
			if err != nil || result == nil {
				return err
			}
			if result.StreamDone {
				if result.ExitCode != 0 {
					return &envelopeAlreadyWritten{code: result.ExitCode, cause: result.Cause}
				}
				return nil
			}
			if result.Text != nil {
				result.Text(ios.Out)
			}
			if result.ExitCode != 0 {
				return &envelopeAlreadyWritten{code: result.ExitCode, cause: resultDiagnosticCause(FromCommandResult(result))}
			}
			return nil
		})
		return cmd, nil
	}
	cmd.RunE = Wrap(spec.ID, func(cmd *cobra.Command, args []string) (*CmdResult, error) {
		if generateSkeleton {
			if requestValue, _ := cmd.Flags().GetString("request"); requestValue != "" {
				return nil, output.NewUsageError("REQUEST_FLAG_CONFLICT", "--generate-skeleton cannot be used with --request", "Use one input mode.")
			}
			return skeletonResult(spec.ID)
		}
		result, err := runRegistryModule(cmd, module, spec, args)
		if err != nil {
			return nil, err
		}
		return FromCommandResult(result), nil
	})
	return cmd, nil
}

func runRegistryModule(cmd *cobra.Command, module command.Module, spec command.Spec, args []string) (*command.Result, error) {
	deps := registryRuntimeDeps(spec.ID)
	runtime, err := module.Build(deps)
	if err != nil {
		return nil, err
	}
	if runtime.Handler == nil {
		return nil, fmt.Errorf("command %q runtime missing handler", spec.ID)
	}
	req, err := cmdtree.BuildRequest(cmd, spec, args, deps)
	if err != nil {
		return nil, err
	}
	return runtime.Handler.Run(cmd.Context(), req)
}

func registryRuntimeDeps(commandID string) command.Deps {
	deps := command.Deps{IO: IO()}
	if commandID == "api.call" {
		deps.ControlPlane = controlplane.RawAPIClient{}
	} else {
		deps.ControlPlane = &controlplane.SDK{Warnf: Stderr}
	}
	if deps.IO != nil {
		deps.Stdin = deps.IO.In
	}
	return deps
}

func requestFlagChanged(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	flag := cmd.Flags().Lookup("request")
	return flag != nil && flag.Changed
}

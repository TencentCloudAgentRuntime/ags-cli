//go:build preview

// Package acp exposes a harness as a stdio ACP agent.
package acp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/signal"
	"syscall"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/harness/get"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/acpclient"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
)

// Module returns the stdio bridge workflow.
func Module() command.Module {
	spec := command.Spec{
		ID:    "harness.acp",
		Path:  []string{"harness", "acp"},
		Use:   "acp <harness-id>",
		Short: "Run a harness as a local stdio ACP agent",
		Long: "Bridge newline-delimited ACP JSON-RPC on stdin/stdout to the remote harness, so any ACP client that launches agents as subprocesses (editors, acpx, SDKs) can use the managed harness. The bridge refreshes access tokens and keeps the affinity value for the whole process.\n\n" +
			"stdout carries only ACP messages; diagnostics go to stderr. The process exits when stdin closes. Each process is one ACP connection; closing it does not cancel a running turn or delete history. Available in preview builds.",
		Examples: []string{
			"Example - One-shot prompt through acpx:\n  acpx --agent 'agr harness acp hns-a1b2c3d4' exec 'Summarize /workspace'",
			"Example - Editor agent configuration (command and args):\n  command: agr\n  args: [\"harness\", \"acp\", \"hns-a1b2c3d4\"]",
		},
		Args: []command.ArgSpec{{Name: "harness-id", Required: true, Description: "Harness ID, such as hns-a1b2c3d4."}},
		Flags: []command.FlagSpec{
			harnesscli.StringFlag("affinity-id", "Initial affinity value, to reach the instance of an earlier session"),
		},
		Output: command.OutputSpec{DataType: "ACPStream", Description: "Raw ACP JSON-RPC messages on stdout.", Effects: []string{"read:harness", "read:deployment-token"}},
	}
	return command.Module{
		Descriptor: command.Descriptor{Spec: spec, Groups: harnesscli.GroupsOf(get.APIDescriptor(), 1), Source: command.SourceWorkflow},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			client, err := harnesscli.NewClient(deps, spec.ID)
			if err != nil {
				return command.Runtime{}, err
			}
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				endpoint, target, err := harnesscli.ResolveEndpoint(ctx, deps, client, req.ArgValues["harness-id"], harnesscli.String(req, "affinity-id"))
				if err != nil {
					return nil, err
				}
				fmt.Fprintf(deps.IO.ErrOut, "agr: bridging stdio ACP to %s\n", target.URL())
				signalCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
				defer stop()
				bridge, err := acpclient.ServeStdio(signalCtx, endpoint, req.Stdin, deps.IO.Out)
				if affinity := bridge.Affinity(); affinity != "" {
					fmt.Fprintf(deps.IO.ErrOut, "agr: affinity id %s (reuse with --affinity-id)\n", affinity)
				}
				if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
					return nil, acpclient.Classify(err, fmt.Sprintf("Check that an instance is running with 'agr harness endpoint %s --check'.", target.HarnessID))
				}
				return &command.Result{StreamDone: true}, nil
			})}, nil
		},
	}
}

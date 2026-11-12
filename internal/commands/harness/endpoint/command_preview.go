//go:build preview

// Package endpoint prints the real ACP address of a harness for external clients.
package endpoint

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/harness/get"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/acpclient"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/textview"
)

// Module returns the endpoint workflow.
func Module() command.Module {
	spec := command.Spec{
		ID:    "harness.endpoint",
		Path:  []string{"harness", "endpoint"},
		Use:   "endpoint <harness-id>",
		Short: "Print the ACP URL and headers for connecting from another client",
		Long: "Print the public ACP Streamable HTTP address of a published harness and the headers a client must send. Use it to connect your own program, SDK, or tool directly.\n\n" +
			"Clients must send X-Access-Token and echo the affinity header value returned by the service on every later request, so one conversation stays on one instance.\n\n" +
			"--show-token issues a short-lived Deployment token and prints it; the CLI does not refresh tokens for external clients. For long-running services, call AcquireDeploymentToken from your backend before ExpiresAt. --check performs one ACP initialize handshake to prove an instance is reachable; PUBLISHED alone does not.\n\n" +
			"For browsers and tools that cannot add headers, use 'agr harness proxy'; for stdio ACP clients use 'agr harness acp'. Available in preview builds.",
		Examples: []string{
			"Example - Show the address and required headers:\n  agr harness endpoint hns-a1b2c3d4 -o text",
			"Example - Get the URL and a token for a script:\n  agr harness endpoint hns-a1b2c3d4 --show-token -o json --jq '.Data | {AcpURL, Headers, ExpiresAt}'",
			"Example - Verify that an instance answers ACP initialize:\n  agr harness endpoint hns-a1b2c3d4 --check -o text",
		},
		SupportsJSON: true,
		Args:         []command.ArgSpec{{Name: "harness-id", Required: true, Description: "Harness ID, such as hns-a1b2c3d4."}},
		Flags: []command.FlagSpec{
			harnesscli.BoolFlag("show-token", "Issue a short-lived Deployment access token and include it in the output"),
			harnesscli.BoolFlag("check", "Run one ACP initialize handshake against the endpoint"),
		},
		Output: command.OutputSpec{DataType: "HarnessEndpointData", Description: "ACP URL, required headers, and optionally a token and check result.", Effects: []string{"read:harness", "read:deployment-token"}},
	}
	return command.Module{
		Descriptor: command.Descriptor{Spec: spec, Groups: harnesscli.GroupsOf(get.APIDescriptor(), 1), Source: command.SourceWorkflow},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			client, err := harnesscli.NewClient(deps, spec.ID)
			if err != nil {
				return command.Runtime{}, err
			}
			plane, ok := deps.ControlPlane.(harnesscli.DeploymentPlane)
			if !ok {
				return command.Runtime{}, fmt.Errorf("%s requires Deployment control-plane support", spec.ID)
			}
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				return run(ctx, client, plane, req)
			})}, nil
		},
	}
}

func run(ctx context.Context, client harnesscli.Client, plane harnesscli.DeploymentPlane, req command.Request) (*command.Result, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	target, err := harnesscli.ResolveTarget(ctx, client, plane, req.ArgValues["harness-id"])
	if err != nil {
		return nil, err
	}
	wantToken, check := harnesscli.Bool(req, "show-token"), harnesscli.Bool(req, "check")
	data := map[string]any{
		"HarnessId":         target.HarnessID,
		"DeploymentId":      target.DeploymentID,
		"AcpURL":            target.URL(),
		"Transport":         "acp-streamable-http",
		"AuthHeader":        harnesscli.AccessTokenHeader,
		"AffinityHeader":    target.AffinityHeader,
		"TokenCommand":      fmt.Sprintf("agr harness endpoint %s --show-token -o json --jq '.Data.Headers'", target.HarnessID),
		"LocalProxyCommand": fmt.Sprintf("agr harness proxy %s", target.HarnessID),
	}
	var token harnesscli.Token
	if wantToken || check {
		token, err = harnesscli.AcquireToken(ctx, plane, target.DeploymentID)
		if err != nil {
			return nil, err
		}
		if !wantToken {
			cli.MaskSecret(token.Value)
		}
	}
	if wantToken {
		data["Headers"] = map[string]string{harnesscli.AccessTokenHeader: token.Value}
		data["ExpiresAt"] = token.ExpiresAt
	}
	var checkErr error
	if check {
		started := time.Now()
		info, err := acpclient.Probe(ctx, acpclient.Endpoint{URL: target.URL(), Token: func(context.Context) (string, error) { return token.Value, nil }, AffinityHeader: target.AffinityHeader})
		result := map[string]any{"Reachable": err == nil, "DurationMs": time.Since(started).Milliseconds()}
		if err != nil {
			result["Error"] = err.Error()
			checkErr = err
		} else {
			result["Agent"] = info
		}
		data["Check"] = result
	}
	out := &command.Result{Data: data, Text: func(w io.Writer) { render(w, data, wantToken) }}
	if checkErr != nil {
		classified := output.ClassifyError(acpclient.Classify(checkErr, fmt.Sprintf("The Deployment may still be starting an instance. Check 'agr harness status %s', then retry --check.", target.HarnessID)))
		out.Failure = classified.Failure
		out.ExitCode = classified.ExitCode
	}
	return out, nil
}

func render(w io.Writer, data map[string]any, withToken bool) {
	rows := []textview.KeyValue{
		{Key: "ACP URL", Value: fmt.Sprint(data["AcpURL"])},
		{Key: "Transport", Value: "ACP Streamable HTTP"},
	}
	if withToken {
		headers, _ := data["Headers"].(map[string]string)
		rows = append(rows, textview.KeyValue{Key: "Auth header", Value: fmt.Sprintf("%s: %s", harnesscli.AccessTokenHeader, headers[harnesscli.AccessTokenHeader])},
			textview.KeyValue{Key: "Expires", Value: fmt.Sprint(data["ExpiresAt"])})
	} else {
		rows = append(rows, textview.KeyValue{Key: "Auth header", Value: harnesscli.AccessTokenHeader + "   (add --show-token to issue one)"})
	}
	if header, _ := data["AffinityHeader"].(string); header != "" {
		rows = append(rows, textview.KeyValue{Key: "Affinity", Value: header + "   (echo the value returned in responses)"})
	}
	rows = append(rows, textview.KeyValue{Key: "Deployment", Value: fmt.Sprint(data["DeploymentId"])})
	if check, ok := data["Check"].(map[string]any); ok {
		status := fmt.Sprintf("reachable in %dms", check["DurationMs"])
		if reachable, _ := check["Reachable"].(bool); !reachable {
			status = fmt.Sprint("failed: ", check["Error"])
		}
		rows = append(rows, textview.KeyValue{Key: "Check", Value: status})
	}
	rows = append(rows, textview.KeyValue{Key: "Local proxy", Value: fmt.Sprint(data["LocalProxyCommand"])})
	textview.PrintKV(w, rows)
}

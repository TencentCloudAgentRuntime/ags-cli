//go:build preview

// Package web opens the native DSH console through Deployment Webview.
package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/harness/get"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/textview"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/utils"
)

// Module returns the native DSH Web console workflow.
func Module() command.Module {
	spec := command.Spec{
		ID:    "harness.web",
		Path:  []string{"harness", "web"},
		Use:   "web <harness-id>",
		Short: "Open the native DSH Web console in your browser",
		Long: "Open the native DSH console at the Deployment's HTTPS Webview entry on service port 3080, using the configured region and data-plane domain. Requires a PUBLISHED DSH revision and an ACTIVE Deployment. The draft engine does not determine the live console.\n\n" +
			"By default, acquire a Deployment token, wait for HTTP 200 on the native console, start the browser on the machine running the CLI, and exit. Preparation and readiness have a 90s deadline (change with --timeout); Ctrl+C cancels the wait. Only startup HTTP 404/502/503 responses are retried. The confirmed affinity is carried into the browser URL. Opened means the browser launch was requested; it does not prove the browser rendered the page. The readiness request may start an instance according to the Deployment policy. No local proxy or persistent CLI process is needed.\n\n" +
			"--no-open prints only the safe URL and does not acquire a token unless --show-token is also set. --show-token explicitly includes AccessURL and ExpiresAt in text/JSON output; treat the link as a credential. For SSH or headless use, combine --no-open --show-token and copy AccessURL to your browser. Rerun the command when the token or browser authentication expires.\n\n" +
			"--affinity-id sets the initial affinity_id query parameter. The gateway maintains browser affinity using a cookie shared by tabs on the same host; tabs are not isolated instances. Available in preview builds.",
		Examples: []string{
			"Example - Open a published DSH console:\n  agr harness web hns-a1b2c3d4 -o text",
			"Example - Print the safe URL without acquiring a token:\n  agr harness web hns-a1b2c3d4 --no-open -o json",
			"Example - Get a login link on an SSH host:\n  agr harness web hns-a1b2c3d4 --no-open --show-token -o json --jq '.Data | {AccessURL, ExpiresAt}'",
			"Example - Request an existing affinity when entering the console:\n  agr harness web hns-a1b2c3d4 --affinity-id conversation-1 -o text",
		},
		SupportsJSON: true,
		Args:         []command.ArgSpec{{Name: "harness-id", Required: true, Description: "Harness ID, such as hns-a1b2c3d4."}},
		Flags: []command.FlagSpec{
			harnesscli.BoolFlag("no-open", "Print the URL without starting a browser; acquire no token unless --show-token is set"),
			harnesscli.BoolFlag("show-token", "Acquire a Deployment token and include the credential-bearing AccessURL and ExpiresAt in output"),
			harnesscli.StringFlag("affinity-id", "Set affinity_id on the initial Webview URL; browser tabs on the same host share the affinity cookie"),
			harnesscli.StringFlag("timeout", "Maximum time for preparation and Web readiness before browser launch, such as 2m (default 90s); --no-open skips readiness"),
		},
		Output: command.OutputSpec{DataType: "HarnessWebData", Description: "Safe Webview URL, published revision, browser launch status, and optionally AccessURL and ExpiresAt.", Effects: []string{"read:harness", "read:deployment", "read:deployment-token"}},
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
				return run(ctx, client, plane, req, utils.OpenBrowser)
			})}, nil
		},
	}
}

func run(ctx context.Context, client harnesscli.Client, plane harnesscli.DeploymentPlane, req command.Request, openBrowser func(string) error) (*command.Result, error) {
	timeout := 90 * time.Second
	if raw := harnesscli.String(req, "timeout"); raw != "" {
		var err error
		if timeout, err = time.ParseDuration(raw); err != nil || timeout <= 0 {
			return nil, output.NewUsageError("INVALID_TIMEOUT", "Invalid --timeout", "Use a positive Go duration such as 90s or 2m.")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	harnessID := req.ArgValues["harness-id"]
	group, readCtx := errgroup.WithContext(ctx)
	var target harnesscli.Target
	var revisions []apivalue.Object
	group.Go(func() error {
		var err error
		target, err = harnesscli.ResolveTarget(readCtx, client, plane, harnessID)
		return err
	})
	group.Go(func() error {
		var err error
		revisions, err = client.Revisions(readCtx, harnessID)
		return err
	})
	if err := group.Wait(); err != nil {
		return nil, err
	}
	live, _ := harnesscli.LiveRevision(revisions)
	if live == nil {
		return nil, output.NewConflictError("HARNESS_NOT_PUBLISHED", "The Harness has no PUBLISHED revision",
			fmt.Sprintf("Check 'agr harness status %s' and publish a DSH revision before opening the console.", harnessID))
	}
	if engine := live.Object("Spec").String("HarnessEngine"); engine != "dsh" {
		return nil, output.NewConflictError("HARNESS_WEB_UNSUPPORTED_ENGINE", fmt.Sprintf("Published revision %d uses engine %q; the Web console is available only for dsh", live.Int64("Revision"), engine),
			"Use a Harness with a published DSH revision. For ACP clients, use 'agr harness endpoint' or 'agr harness proxy'.")
	}
	if target.DeploymentStatus != "ACTIVE" {
		return nil, output.NewConflictError("DEPLOYMENT_NOT_ACTIVE", fmt.Sprintf("Deployment %s is not ACTIVE (status %q)", target.DeploymentID, target.DeploymentStatus),
			fmt.Sprintf("Check 'agr deployment get %s' and retry once the Deployment is ACTIVE.", target.DeploymentID))
	}
	webURL, err := url.Parse(target.WebURL())
	if err != nil {
		return nil, err
	}
	query := webURL.Query()
	if affinityID := harnesscli.String(req, "affinity-id"); affinityID != "" {
		query.Set("affinity_id", affinityID)
	}
	webURL.RawQuery = query.Encode()
	data := map[string]any{
		"HarnessId": target.HarnessID, "DeploymentId": target.DeploymentID,
		"Revision": live.Int64("Revision"), "WebURL": webURL.String(), "Opened": false,
	}
	noOpen, showToken := harnesscli.Bool(req, "no-open"), harnesscli.Bool(req, "show-token")
	if !noOpen || showToken {
		token, err := harnesscli.AcquireToken(ctx, plane, target.DeploymentID)
		if err != nil {
			return nil, err
		}
		cli.MaskSecret(token.Value)
		query.Set("token", token.Value)
		webURL.RawQuery = query.Encode()
		if !noOpen {
			affinity, err := waitForWeb(ctx, webURL, http.DefaultTransport, time.Second)
			if err != nil {
				return nil, err
			}
			if affinity != "" {
				query.Set("affinity_id", affinity)
				webURL.RawQuery = query.Encode()
				safeQuery := webURL.Query()
				safeQuery.Del("token")
				safeURL := *webURL
				safeURL.RawQuery = safeQuery.Encode()
				data["WebURL"] = safeURL.String()
			}
		}
		if showToken {
			data["AccessURL"], data["ExpiresAt"] = webURL.String(), token.ExpiresAt
		}
		if !noOpen {
			if err := openBrowser(webURL.String()); err != nil {
				// Launcher errors can contain the full command and credential URL.
				return nil, output.NewCLIError(&output.Failure{
					Code: "BROWSER_OPEN_FAILED", Kind: output.KindGenericError,
					Message: "Could not start the browser on this machine",
					Hint:    fmt.Sprintf("Run 'agr harness web %s --no-open --show-token -o text', then copy AccessURL to your browser.", harnessID),
				})
			}
			data["Opened"] = true
		}
	}
	return &command.Result{Data: data, Text: func(w io.Writer) { render(w, data) }}, nil
}

func render(w io.Writer, data map[string]any) {
	rows := []textview.KeyValue{
		{Key: "Web URL", Value: fmt.Sprint(data["WebURL"])},
		{Key: "Harness", Value: fmt.Sprint(data["HarnessId"])},
		{Key: "Deployment", Value: fmt.Sprint(data["DeploymentId"])},
		{Key: "Revision", Value: fmt.Sprint(data["Revision"])},
		{Key: "Browser launch requested", Value: fmt.Sprint(data["Opened"])},
	}
	if accessURL, ok := data["AccessURL"].(string); ok {
		rows = append(rows, textview.KeyValue{Key: "AccessURL (credential)", Value: accessURL}, textview.KeyValue{Key: "Expires", Value: fmt.Sprint(data["ExpiresAt"])})
	}
	textview.PrintKV(w, rows)
}

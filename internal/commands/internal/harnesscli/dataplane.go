//go:build preview

package harnesscli

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	deploymentproxy "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/deployment/proxy"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/acpclient"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
)

// DataPlaneKey lets tests inject an ACP endpoint (for example a loopback
// server) instead of resolving the real Deployment address.
const DataPlaneKey = "harnesscli.endpoint"

// ResolveEndpoint returns a refreshing ACP endpoint for a harness.
func ResolveEndpoint(ctx context.Context, deps command.Deps, client Client, harnessID, affinityID string) (acpclient.Endpoint, Target, error) {
	return resolveEndpoint(ctx, deps, client, harnessID, affinityID, false)
}

// PrepareEndpoint resolves the Deployment and prefetches its token concurrently
// for prompt, which always sends initialize next. Stdio ACP stays lazy.
func PrepareEndpoint(ctx context.Context, deps command.Deps, client Client, harnessID, affinityID string) (acpclient.Endpoint, Target, error) {
	return resolveEndpoint(ctx, deps, client, harnessID, affinityID, true)
}

func resolveEndpoint(ctx context.Context, deps command.Deps, client Client, harnessID, affinityID string, prefetch bool) (acpclient.Endpoint, Target, error) {
	if injected, ok := deps.Values[DataPlaneKey].(func(string) (acpclient.Endpoint, Target)); ok {
		endpoint, target := injected(harnessID)
		endpoint.AffinityID = affinityID
		return endpoint, target, nil
	}
	if err := config.Validate(); err != nil {
		return acpclient.Endpoint{}, Target{}, err
	}
	plane, ok := deps.ControlPlane.(deploymentproxy.ControlPlane)
	if !ok {
		return acpclient.Endpoint{}, Target{}, fmt.Errorf("harness data-plane commands require Deployment control-plane support")
	}
	var target Target
	var token func(context.Context) (string, error)
	lifecycle := context.WithoutCancel(ctx)
	if prefetch {
		deploymentID, err := resolveDeploymentID(ctx, client, harnessID)
		if err != nil {
			return acpclient.Endpoint{}, Target{}, err
		}
		group, prepareCtx := errgroup.WithContext(ctx)
		group.Go(func() error {
			var err error
			target, err = resolveDeploymentTarget(prepareCtx, plane, harnessID, deploymentID)
			return err
		})
		group.Go(func() error {
			var err error
			token, err = deploymentproxy.NewPrefetchedTokenSource(prepareCtx, lifecycle, plane, deploymentID, deps.Now)
			if err != nil {
				return acpclient.Classify(err, fmt.Sprintf("Check the Deployment token permission and 'agr harness endpoint %s --check'.", harnessID))
			}
			return nil
		})
		if err := group.Wait(); err != nil {
			return acpclient.Endpoint{}, Target{}, err
		}
	} else {
		var err error
		target, err = ResolveTarget(ctx, client, plane, harnessID)
		if err != nil {
			return acpclient.Endpoint{}, Target{}, err
		}
		token = deploymentproxy.NewTokenSource(lifecycle, plane, target.DeploymentID, deps.Now)
	}
	return acpclient.Endpoint{
		URL:            target.URL(),
		Token:          token,
		AffinityHeader: target.AffinityHeader,
		AffinityID:     affinityID,
	}, target, nil
}

//go:build preview

package harnesscli

import (
	"context"
	"fmt"

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
	target, err := ResolveTarget(ctx, client, plane, harnessID)
	if err != nil {
		return acpclient.Endpoint{}, Target{}, err
	}
	return acpclient.Endpoint{
		URL:            target.URL(),
		Token:          deploymentproxy.NewTokenSource(context.WithoutCancel(ctx), plane, target.DeploymentID, nil),
		AffinityHeader: target.AffinityHeader,
		AffinityID:     affinityID,
	}, target, nil
}

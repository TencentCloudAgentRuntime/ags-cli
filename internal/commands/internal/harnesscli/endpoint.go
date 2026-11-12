//go:build preview

package harnesscli

import (
	"context"
	"fmt"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// ACPPort and ACPPath are fixed by the managed Harness image contract.
const (
	ACPPort               = 8080
	ACPPath               = "/acp"
	AccessTokenHeader     = "X-Access-Token"
	defaultAffinityHeader = "X-Tencent-Agr-Affinity-Id"
)

// DeploymentPlane is the Deployment capability shared with deployment proxy.
type DeploymentPlane interface {
	GetDeployment(context.Context, string) (apivalue.Object, error)
	GetDeploymentToken(context.Context, string) (apivalue.Object, error)
}

// Target identifies the ACP entry of a harness.
type Target struct {
	HarnessID      string
	DeploymentID   string
	Domain         string
	AffinityHeader string
}

// URL is the public ACP URL of the Deployment.
func (t Target) URL() string {
	return fmt.Sprintf("https://%d-%s.%s%s", ACPPort, t.DeploymentID, t.Domain, ACPPath)
}

// ResolveTarget maps a harness to its Deployment and affinity header.
func ResolveTarget(ctx context.Context, client Client, plane DeploymentPlane, harnessID string) (Target, error) {
	harness, err := client.Harness(ctx, harnessID)
	if err != nil {
		return Target{}, err
	}
	deploymentID := harness.Object("ResourceInfo").String("DeploymentId")
	if deploymentID == "" {
		return Target{}, output.NewCLIError(&output.Failure{
			Code: "HARNESS_NOT_DEPLOYED", Kind: output.KindConflict,
			Message: fmt.Sprintf("harness %s has no Deployment yet", harnessID),
			Hint:    fmt.Sprintf("The first publish has not created the Deployment. Check with 'agr harness status %s'.", harnessID),
		})
	}
	deployment, err := plane.GetDeployment(ctx, deploymentID)
	if err != nil {
		return Target{}, fmt.Errorf("failed to get deployment %s: %w", deploymentID, err)
	}
	cfg := config.Get()
	return Target{
		HarnessID:      harnessID,
		DeploymentID:   deploymentID,
		Domain:         fmt.Sprintf("%s.agents.%s", cfg.Region, cfg.DataPlaneDomain()),
		AffinityHeader: AffinityHeader(deployment),
	}, nil
}

// AffinityHeader returns the Deployment affinity header, or "" when disabled.
func AffinityHeader(deployment apivalue.Object) string {
	affinity := deployment.Object("AffinityConfiguration")
	if strings.TrimSpace(affinity.String("Mode")) == "" {
		return ""
	}
	if header := affinity.String("HeaderName"); header != "" {
		return header
	}
	return defaultAffinityHeader
}

// Token is a short-lived Deployment access token.
type Token struct {
	Value     string
	ExpiresAt string
}

// AcquireToken requests a new Deployment access token.
func AcquireToken(ctx context.Context, plane DeploymentPlane, deploymentID string) (Token, error) {
	value, err := plane.GetDeploymentToken(ctx, deploymentID)
	if err != nil {
		return Token{}, err
	}
	token := Token{Value: value.String("Token"), ExpiresAt: value.String("ExpiresAt")}
	if token.Value == "" {
		return Token{}, fmt.Errorf("AcquireDeploymentToken returned no token")
	}
	return token, nil
}

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

// ACPPort, ACPPath and DSHWebPort are fixed by the managed Harness image contract.
const (
	ACPPort               = 8080
	ACPPath               = "/acp"
	DSHWebPort            = 3080
	AccessTokenHeader     = "X-Access-Token"
	defaultAffinityHeader = "X-Tencent-Agr-Affinity-Id"
)

// DeploymentPlane is the Deployment capability shared with deployment proxy.
type DeploymentPlane interface {
	GetDeployment(context.Context, string) (apivalue.Object, error)
	GetDeploymentToken(context.Context, string) (apivalue.Object, error)
}

// Target identifies the Deployment entries of a harness.
type Target struct {
	HarnessID        string
	DeploymentID     string
	DeploymentStatus string
	Domain           string
	AffinityHeader   string
}

// URL is the public ACP URL of the Deployment.
func (t Target) URL() string {
	return fmt.Sprintf("https://%d-%s.%s%s", ACPPort, t.DeploymentID, t.Domain, ACPPath)
}

// WebURL is the DSH browser entry, without an access token.
func (t Target) WebURL() string {
	return fmt.Sprintf("https://%d-%s-webview.%s/", DSHWebPort, t.DeploymentID, t.Domain)
}

// ResolveTarget maps a harness to its Deployment and affinity header.
func ResolveTarget(ctx context.Context, client Client, plane DeploymentPlane, harnessID string) (Target, error) {
	deploymentID, err := resolveDeploymentID(ctx, client, harnessID)
	if err != nil {
		return Target{}, err
	}
	return resolveDeploymentTarget(ctx, plane, harnessID, deploymentID)
}

func resolveDeploymentID(ctx context.Context, client Client, harnessID string) (string, error) {
	harness, err := client.Harness(ctx, harnessID)
	if err != nil {
		return "", err
	}
	deploymentID := harness.Object("ResourceInfo").String("DeploymentId")
	if deploymentID == "" {
		return "", output.NewCLIError(&output.Failure{
			Code: "HARNESS_NOT_DEPLOYED", Kind: output.KindConflict,
			Message: fmt.Sprintf("harness %s has no Deployment yet", harnessID),
			Hint:    fmt.Sprintf("The first publish has not created the Deployment. Check with 'agr harness status %s'.", harnessID),
		})
	}
	return deploymentID, nil
}

func resolveDeploymentTarget(ctx context.Context, plane DeploymentPlane, harnessID, deploymentID string) (Target, error) {
	deployment, err := plane.GetDeployment(ctx, deploymentID)
	if err != nil {
		return Target{}, fmt.Errorf("failed to get deployment %s: %w", deploymentID, err)
	}
	cfg := config.Get()
	return Target{
		HarnessID:        harnessID,
		DeploymentID:     deploymentID,
		DeploymentStatus: deployment.String("Status"),
		Domain:           fmt.Sprintf("%s.agents.%s", cfg.Region, cfg.DataPlaneDomain()),
		AffinityHeader:   AffinityHeader(deployment),
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
		return Token{}, output.NewCLIError(&output.Failure{
			Code: "EMPTY_DEPLOYMENT_TOKEN", Kind: output.KindGenericError,
			Message: "AcquireDeploymentToken returned no token",
			Hint:    "Retry acquiring the Deployment token. If it is still empty, check the Deployment service response.",
		})
	}
	return token, nil
}

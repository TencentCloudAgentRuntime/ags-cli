package client

import (
	"fmt"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
)

// NewCloudClient creates the cloud-only Tencent Cloud AGS SDK client.
func NewCloudClient() (*ags.Client, error) {
	cfg := config.Get()
	credential := newTencentCloudCredential()
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = cfg.ControlPlaneEndpoint()

	sdk, err := ags.NewClient(credential, cfg.Region, cpf)
	if err != nil {
		return nil, fmt.Errorf("failed to create AGS client: %w", err)
	}
	return sdk, nil
}

func newTencentCloudCredential() *common.Credential {
	if token := config.GetToken(); token != "" {
		return common.NewTokenCredential(config.GetSecretID(), config.GetSecretKey(), token)
	}
	return common.NewCredential(config.GetSecretID(), config.GetSecretKey())
}

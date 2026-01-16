package client

import (
	"context"
	"fmt"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
)

// CloudAPIKeyClient handles API Key operations using tencentcloud-sdk-go
type CloudAPIKeyClient struct {
	client *ags.Client
}

// NewCloudAPIKeyClient creates a new Cloud API Key client
func NewCloudAPIKeyClient(cfg *config.CloudConfig) (*CloudAPIKeyClient, error) {
	credential := common.NewCredential(cfg.SecretID, cfg.SecretKey)

	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = cfg.ControlPlaneEndpoint()

	client, err := ags.NewClient(credential, cfg.Region, cpf)
	if err != nil {
		return nil, fmt.Errorf("failed to create AGS client: %w", err)
	}

	return &CloudAPIKeyClient{
		client: client,
	}, nil
}

// CreateAPIKey creates a new API key
func (c *CloudAPIKeyClient) CreateAPIKey(ctx context.Context, name string) (*CreateAPIKeyResult, error) {
	request := ags.NewCreateAPIKeyRequest()
	request.Name = &name

	response, err := c.client.CreateAPIKeyWithContext(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to create API key: %w", err)
	}

	return &CreateAPIKeyResult{
		KeyID:  derefString(response.Response.KeyId),
		Name:   derefString(response.Response.Name),
		APIKey: derefString(response.Response.APIKey),
	}, nil
}

// ListAPIKeys returns all API keys
func (c *CloudAPIKeyClient) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	request := ags.NewDescribeAPIKeyListRequest()

	response, err := c.client.DescribeAPIKeyListWithContext(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to list API keys: %w", err)
	}

	keys := make([]APIKey, 0, len(response.Response.APIKeySet))
	for _, k := range response.Response.APIKeySet {
		keys = append(keys, APIKey{
			KeyID:     derefString(k.KeyId),
			Name:      derefString(k.Name),
			Status:    derefString(k.Status),
			MaskedKey: derefString(k.MaskedKey),
			CreatedAt: derefString(k.CreatedAt),
		})
	}

	return keys, nil
}

// DeleteAPIKey deletes an API key
func (c *CloudAPIKeyClient) DeleteAPIKey(ctx context.Context, keyID string) error {
	request := ags.NewDeleteAPIKeyRequest()
	request.KeyId = &keyID

	_, err := c.client.DeleteAPIKeyWithContext(ctx, request)
	if err != nil {
		return fmt.Errorf("failed to delete API key: %w", err)
	}

	return nil
}

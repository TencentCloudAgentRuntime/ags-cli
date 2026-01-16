package client

import (
	"context"
	"fmt"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
)

// CloudToolClient handles Tool operations using tencentcloud-sdk-go (internal)
type CloudToolClient struct {
	client *ags.Client
	region string
}

// NewCloudToolClient creates a new Cloud Tool client
func NewCloudToolClient(cfg *config.CloudConfig) (*CloudToolClient, error) {
	credential := common.NewCredential(cfg.SecretID, cfg.SecretKey)

	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = cfg.ControlPlaneEndpoint()

	client, err := ags.NewClient(credential, cfg.Region, cpf)
	if err != nil {
		return nil, fmt.Errorf("failed to create AGS client: %w", err)
	}

	return &CloudToolClient{
		client: client,
		region: cfg.Region,
	}, nil
}

// GetAGSClient returns the underlying AGS client
func (c *CloudToolClient) GetAGSClient() *ags.Client {
	return c.client
}

// GetRegion returns the configured region
func (c *CloudToolClient) GetRegion() string {
	return c.region
}

// ListTools returns available tools with optional filtering and pagination
func (c *CloudToolClient) ListTools(ctx context.Context, opts *ListToolsOptions) (*ListToolsResult, error) {
	request := ags.NewDescribeSandboxToolListRequest()

	if opts != nil {
		// Set specific tool IDs
		if len(opts.ToolIDs) > 0 {
			toolIDs := make([]*string, len(opts.ToolIDs))
			for i, id := range opts.ToolIDs {
				idCopy := id
				toolIDs[i] = &idCopy
			}
			request.ToolIds = toolIDs
		} else {
			// Pagination (only when ToolIDs not specified)
			if opts.Offset > 0 {
				offset := int64(opts.Offset)
				request.Offset = &offset
			}
			if opts.Limit > 0 {
				limit := int64(opts.Limit)
				request.Limit = &limit
			}

			// Build filters
			var filters []*ags.Filter

			if opts.Status != "" {
				status := opts.Status
				filters = append(filters, &ags.Filter{
					Name:   strPtr("Status"),
					Values: []*string{&status},
				})
			}
			if opts.ToolType != "" {
				toolType := opts.ToolType
				filters = append(filters, &ags.Filter{
					Name:   strPtr("ToolType"),
					Values: []*string{&toolType},
				})
			}
			if opts.CreatedSince != "" {
				createdSince := opts.CreatedSince
				filters = append(filters, &ags.Filter{
					Name:   strPtr("created-since"),
					Values: []*string{&createdSince},
				})
			}
			if opts.CreatedSinceTime != "" {
				createdSinceTime := opts.CreatedSinceTime
				filters = append(filters, &ags.Filter{
					Name:   strPtr("created-since-time"),
					Values: []*string{&createdSinceTime},
				})
			}
			// Tag filters
			for k, v := range opts.Tags {
				tagKey := "tag:" + k
				tagValue := v
				filters = append(filters, &ags.Filter{
					Name:   &tagKey,
					Values: []*string{&tagValue},
				})
			}

			if len(filters) > 0 {
				request.Filters = filters
			}
		}
	}

	response, err := c.client.DescribeSandboxToolListWithContext(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	tools := make([]Tool, 0, len(response.Response.SandboxToolSet))
	for _, t := range response.Response.SandboxToolSet {
		// Parse tags
		tags := make(map[string]string)
		for _, tag := range t.Tags {
			if tag.Key != nil && tag.Value != nil {
				tags[*tag.Key] = *tag.Value
			}
		}

		// Parse storage mounts
		storageMounts := parseAPIStorageMounts(t.StorageMounts)

		// Parse network configuration
		networkMode := ""
		var vpcConfig *VPCConfig
		if t.NetworkConfiguration != nil {
			if t.NetworkConfiguration.NetworkMode != nil {
				networkMode = *t.NetworkConfiguration.NetworkMode
			}
			// Parse VPC config if present
			if t.NetworkConfiguration.VpcConfig != nil {
				vpcConfig = parseAPIVPCConfig(t.NetworkConfiguration.VpcConfig)
			}
		}

		tools = append(tools, Tool{
			ID:            derefString(t.ToolId),
			Name:          derefString(t.ToolName),
			Description:   derefString(t.Description),
			Type:          derefString(t.ToolType),
			NetworkMode:   networkMode,
			VPCConfig:     vpcConfig,
			Tags:          tags,
			RoleArn:       derefString(t.RoleArn),
			StorageMounts: storageMounts,
			CreatedAt:     derefString(t.CreateTime),
		})
	}

	totalCount := 0
	if response.Response.TotalCount != nil {
		totalCount = int(*response.Response.TotalCount)
	}

	return &ListToolsResult{
		Tools:      tools,
		TotalCount: totalCount,
	}, nil
}

// strPtr returns a pointer to the string
func strPtr(s string) *string {
	return &s
}

// GetTool returns a specific tool by ID
func (c *CloudToolClient) GetTool(ctx context.Context, id string) (*Tool, error) {
	result, err := c.ListTools(ctx, &ListToolsOptions{ToolIDs: []string{id}})
	if err != nil {
		return nil, err
	}
	if len(result.Tools) == 0 {
		return nil, fmt.Errorf("tool not found: %s", id)
	}
	return &result.Tools[0], nil
}

// CreateTool creates a new sandbox tool
func (c *CloudToolClient) CreateTool(ctx context.Context, opts *CreateToolOptions) (*Tool, error) {
	request := ags.NewCreateSandboxToolRequest()
	request.ToolName = &opts.Name
	request.ToolType = &opts.Type

	// NetworkConfiguration is required, default to PUBLIC
	networkMode := opts.NetworkMode
	if networkMode == "" {
		networkMode = "PUBLIC"
	}
	request.NetworkConfiguration = &ags.NetworkConfiguration{
		NetworkMode: &networkMode,
	}

	// Set VPC config if NetworkMode is VPC
	if networkMode == "VPC" && opts.VPCConfig != nil {
		request.NetworkConfiguration.VpcConfig = toAPIVPCConfig(opts.VPCConfig)
	}

	if opts.Description != "" {
		request.Description = &opts.Description
	}
	if opts.DefaultTimeout != "" {
		request.DefaultTimeout = &opts.DefaultTimeout
	}
	if len(opts.Tags) > 0 {
		tags := make([]*ags.Tag, 0, len(opts.Tags))
		for k, v := range opts.Tags {
			key := k
			value := v
			tags = append(tags, &ags.Tag{
				Key:   &key,
				Value: &value,
			})
		}
		request.Tags = tags
	}

	// Set RoleArn for COS access
	if opts.RoleArn != "" {
		request.RoleArn = &opts.RoleArn
	}

	// Set StorageMounts
	if len(opts.StorageMounts) > 0 {
		request.StorageMounts = toAPIStorageMounts(opts.StorageMounts)
	}

	response, err := c.client.CreateSandboxToolWithContext(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to create tool: %w", err)
	}

	return &Tool{
		ID:            derefString(response.Response.ToolId),
		Name:          opts.Name,
		Description:   opts.Description,
		Type:          opts.Type,
		NetworkMode:   networkMode,
		VPCConfig:     opts.VPCConfig,
		RoleArn:       opts.RoleArn,
		StorageMounts: opts.StorageMounts,
	}, nil
}

// toAPIStorageMounts converts client StorageMounts to API format
func toAPIStorageMounts(mounts []StorageMount) []*ags.StorageMount {
	if len(mounts) == 0 {
		return nil
	}

	result := make([]*ags.StorageMount, len(mounts))
	for i, m := range mounts {
		apiMount := &ags.StorageMount{
			Name:      &m.Name,
			MountPath: &m.MountPath,
			ReadOnly:  &m.ReadOnly,
		}

		if m.StorageSource != nil && m.StorageSource.Cos != nil {
			apiMount.StorageSource = &ags.StorageSource{
				Cos: &ags.CosStorageSource{
					BucketName: &m.StorageSource.Cos.BucketName,
					BucketPath: &m.StorageSource.Cos.BucketPath,
				},
			}
			if m.StorageSource.Cos.Endpoint != "" {
				apiMount.StorageSource.Cos.Endpoint = &m.StorageSource.Cos.Endpoint
			}
		}

		result[i] = apiMount
	}

	return result
}

// toAPIVPCConfig converts client VPCConfig to API format
func toAPIVPCConfig(vpc *VPCConfig) *ags.VPCConfig {
	if vpc == nil {
		return nil
	}

	result := &ags.VPCConfig{}

	if len(vpc.SubnetIds) > 0 {
		subnetIds := make([]*string, len(vpc.SubnetIds))
		for i, id := range vpc.SubnetIds {
			idCopy := id
			subnetIds[i] = &idCopy
		}
		result.SubnetIds = subnetIds
	}

	if len(vpc.SecurityGroupIds) > 0 {
		sgIds := make([]*string, len(vpc.SecurityGroupIds))
		for i, id := range vpc.SecurityGroupIds {
			idCopy := id
			sgIds[i] = &idCopy
		}
		result.SecurityGroupIds = sgIds
	}

	return result
}

// parseAPIVPCConfig converts API VPCConfig to client format
func parseAPIVPCConfig(apiVPC *ags.VPCConfig) *VPCConfig {
	if apiVPC == nil {
		return nil
	}

	vpc := &VPCConfig{}

	if len(apiVPC.SubnetIds) > 0 {
		vpc.SubnetIds = make([]string, len(apiVPC.SubnetIds))
		for i, id := range apiVPC.SubnetIds {
			vpc.SubnetIds[i] = derefString(id)
		}
	}

	if len(apiVPC.SecurityGroupIds) > 0 {
		vpc.SecurityGroupIds = make([]string, len(apiVPC.SecurityGroupIds))
		for i, id := range apiVPC.SecurityGroupIds {
			vpc.SecurityGroupIds[i] = derefString(id)
		}
	}

	return vpc
}

// parseAPIStorageMounts converts API StorageMounts to client format
func parseAPIStorageMounts(apiMounts []*ags.StorageMount) []StorageMount {
	if len(apiMounts) == 0 {
		return nil
	}

	result := make([]StorageMount, len(apiMounts))
	for i, m := range apiMounts {
		mount := StorageMount{
			Name:      derefString(m.Name),
			MountPath: derefString(m.MountPath),
			ReadOnly:  m.ReadOnly != nil && *m.ReadOnly,
		}

		if m.StorageSource != nil && m.StorageSource.Cos != nil {
			mount.StorageSource = &StorageSource{
				Cos: &CosStorageSource{
					Endpoint:   derefString(m.StorageSource.Cos.Endpoint),
					BucketName: derefString(m.StorageSource.Cos.BucketName),
					BucketPath: derefString(m.StorageSource.Cos.BucketPath),
				},
			}
		}

		result[i] = mount
	}

	return result
}

// UpdateTool updates a sandbox tool
func (c *CloudToolClient) UpdateTool(ctx context.Context, opts *UpdateToolOptions) error {
	request := ags.NewUpdateSandboxToolRequest()
	request.ToolId = &opts.ToolID

	if opts.Description != nil {
		request.Description = opts.Description
	}

	if opts.NetworkMode != nil {
		request.NetworkConfiguration = &ags.NetworkConfiguration{
			NetworkMode: opts.NetworkMode,
		}
	}

	if opts.Tags != nil {
		tags := make([]*ags.Tag, 0, len(opts.Tags))
		for k, v := range opts.Tags {
			key := k
			value := v
			tags = append(tags, &ags.Tag{
				Key:   &key,
				Value: &value,
			})
		}
		request.Tags = tags
	}

	_, err := c.client.UpdateSandboxToolWithContext(ctx, request)
	if err != nil {
		return fmt.Errorf("failed to update tool: %w", err)
	}

	return nil
}

// DeleteTool deletes a sandbox tool
func (c *CloudToolClient) DeleteTool(ctx context.Context, id string) error {
	request := ags.NewDeleteSandboxToolRequest()
	request.ToolId = &id

	_, err := c.client.DeleteSandboxToolWithContext(ctx, request)
	if err != nil {
		return fmt.Errorf("failed to delete tool: %w", err)
	}

	return nil
}

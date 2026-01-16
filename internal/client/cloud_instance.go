package client

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"

	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
)

// CloudInstanceClient handles Instance operations using tencentcloud-sdk-go
type CloudInstanceClient struct {
	client          *ags.Client
	cfg             *config.CloudConfig
	region          string
	dataPlaneDomain string
	dataPlane       *DataPlaneClient
}

// NewCloudInstanceClient creates a new Cloud Instance client
func NewCloudInstanceClient(cfg *config.CloudConfig) (*CloudInstanceClient, error) {
	credential := common.NewCredential(cfg.SecretID, cfg.SecretKey)
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = cfg.ControlPlaneEndpoint()

	client, err := ags.NewClient(credential, cfg.Region, cpf)
	if err != nil {
		return nil, fmt.Errorf("failed to create AGS client: %w", err)
	}

	return &CloudInstanceClient{
		client:          client,
		cfg:             cfg,
		region:          cfg.Region,
		dataPlaneDomain: cfg.DataPlaneDomain(),
		dataPlane:       NewDataPlaneClient(&http.Client{Timeout: 60 * time.Second}),
	}, nil
}

// CreateInstance creates a new sandbox instance
func (c *CloudInstanceClient) CreateInstance(ctx context.Context, opts *CreateInstanceOptions) (*Instance, error) {
	toolName := opts.ToolName
	if toolName == "" {
		toolName = "code-interpreter-v1"
	}

	// Set timeout duration
	timeout := time.Duration(opts.Timeout) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	request := ags.NewStartSandboxInstanceRequest()
	request.ToolName = &toolName

	// Set timeout
	timeoutStr := formatDuration(timeout)
	request.Timeout = &timeoutStr

	// Set MountOptions if specified
	if len(opts.MountOptions) > 0 {
		request.MountOptions = toAPIMountOptions(opts.MountOptions)
	}

	response, err := c.client.StartSandboxInstanceWithContext(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to create instance: %w", err)
	}

	inst := response.Response.Instance
	if inst == nil {
		return nil, fmt.Errorf("no instance returned from API")
	}

	// Parse MountOptions from response
	respMountOptions := parseAPIMountOptions(inst.MountOptions)

	return &Instance{
		ID:           derefString(inst.InstanceId),
		ToolID:       derefString(inst.ToolId),
		ToolName:     derefString(inst.ToolName),
		Status:       derefString(inst.Status),
		CreatedAt:    derefString(inst.CreateTime),
		Domain:       c.dataPlaneDomain,
		MountOptions: respMountOptions,
	}, nil
}

// toAPIMountOptions converts client MountOptions to API format
func toAPIMountOptions(opts []MountOption) []*ags.MountOption {
	if len(opts) == 0 {
		return nil
	}

	result := make([]*ags.MountOption, len(opts))
	for i, opt := range opts {
		apiOpt := &ags.MountOption{
			Name: &opt.Name,
		}
		if opt.MountPath != "" {
			apiOpt.MountPath = &opt.MountPath
		}
		if opt.SubPath != "" {
			apiOpt.SubPath = &opt.SubPath
		}
		if opt.ReadOnly != nil {
			apiOpt.ReadOnly = opt.ReadOnly
		}
		result[i] = apiOpt
	}

	return result
}

// parseAPIMountOptions converts API MountOptions to client format
func parseAPIMountOptions(apiOpts []*ags.MountOption) []MountOption {
	if len(apiOpts) == 0 {
		return nil
	}

	result := make([]MountOption, len(apiOpts))
	for i, opt := range apiOpts {
		mountOpt := MountOption{
			Name:      derefString(opt.Name),
			MountPath: derefString(opt.MountPath),
			SubPath:   derefString(opt.SubPath),
		}
		if opt.ReadOnly != nil {
			readOnly := *opt.ReadOnly
			mountOpt.ReadOnly = &readOnly
		}
		result[i] = mountOpt
	}

	return result
}

// formatDuration formats duration to string like "5m", "300s", "1h"
func formatDuration(d time.Duration) string {
	if d >= time.Hour && d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	if d >= time.Minute && d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

// ListInstances returns sandbox instances with optional filters
func (c *CloudInstanceClient) ListInstances(ctx context.Context, opts *ListInstancesOptions) (*ListInstancesResult, error) {
	request := ags.NewDescribeSandboxInstanceListRequest()

	if opts != nil {
		// Set instance IDs if specified
		if len(opts.InstanceIDs) > 0 {
			request.InstanceIds = make([]*string, len(opts.InstanceIDs))
			for i, id := range opts.InstanceIDs {
				request.InstanceIds[i] = strPtr(id)
			}
		} else {
			// Set ToolId filter
			if opts.ToolID != "" {
				request.ToolId = &opts.ToolID
			}

			// Set pagination
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
				filters = append(filters, &ags.Filter{
					Name:   strPtr("Status"),
					Values: []*string{strPtr(opts.Status)},
				})
			}
			if opts.CreatedSince != "" {
				filters = append(filters, &ags.Filter{
					Name:   strPtr("created-since"),
					Values: []*string{strPtr(opts.CreatedSince)},
				})
			}
			if opts.CreatedSinceTime != "" {
				filters = append(filters, &ags.Filter{
					Name:   strPtr("created-since-time"),
					Values: []*string{strPtr(opts.CreatedSinceTime)},
				})
			}
			if len(filters) > 0 {
				request.Filters = filters
			}
		}
	}

	response, err := c.client.DescribeSandboxInstanceListWithContext(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to list instances: %w", err)
	}

	result := &ListInstancesResult{
		Instances:  make([]Instance, 0, len(response.Response.InstanceSet)),
		TotalCount: derefInt(response.Response.TotalCount),
	}

	for _, inst := range response.Response.InstanceSet {
		result.Instances = append(result.Instances, parseInstance(inst, c.dataPlaneDomain))
	}

	return result, nil
}

// parseInstance converts API instance to client Instance type
func parseInstance(inst *ags.SandboxInstance, dataPlaneDomain string) Instance {
	instance := Instance{
		ID:             derefString(inst.InstanceId),
		ToolID:         derefString(inst.ToolId),
		ToolName:       derefString(inst.ToolName),
		Status:         derefString(inst.Status),
		CreatedAt:      derefString(inst.CreateTime),
		UpdatedAt:      derefString(inst.UpdateTime),
		ExpiresAt:      derefString(inst.ExpiresAt),
		StopReason:     derefString(inst.StopReason),
		TimeoutSeconds: inst.TimeoutSeconds,
		Domain:         dataPlaneDomain,
		MountOptions:   parseAPIMountOptions(inst.MountOptions),
	}

	return instance
}

// GetInstance returns a specific instance by ID
func (c *CloudInstanceClient) GetInstance(ctx context.Context, id string) (*Instance, error) {
	result, err := c.ListInstances(ctx, &ListInstancesOptions{
		InstanceIDs: []string{id},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}

	if len(result.Instances) == 0 {
		return nil, fmt.Errorf("instance not found: %s", id)
	}

	inst := result.Instances[0]
	return &inst, nil
}

// DeleteInstance deletes a sandbox instance
func (c *CloudInstanceClient) DeleteInstance(ctx context.Context, id string) error {
	request := ags.NewStopSandboxInstanceRequest()
	request.InstanceId = &id

	_, err := c.client.StopSandboxInstanceWithContext(ctx, request)
	if err != nil {
		return fmt.Errorf("failed to delete instance: %w", err)
	}
	return nil
}

// Execute runs code in a sandbox instance
func (c *CloudInstanceClient) Execute(ctx context.Context, instanceID string, codeStr string, language string) (*ExecuteResult, error) {
	return c.ExecuteStream(ctx, instanceID, codeStr, language, nil)
}

// ExecuteStream runs code with streaming output callbacks via data plane HTTP API
func (c *CloudInstanceClient) ExecuteStream(ctx context.Context, instanceID string, codeStr string, language string, callbacks *StreamCallbacks) (*ExecuteResult, error) {
	// Acquire token using public SDK
	tokenResp, err := c.client.AcquireSandboxInstanceTokenWithContext(ctx, &ags.AcquireSandboxInstanceTokenRequest{
		InstanceId: &instanceID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to acquire token: %w", err)
	}
	if tokenResp.Response == nil || tokenResp.Response.Token == nil {
		return nil, fmt.Errorf("no token returned from API")
	}

	// Build sandbox domain: {port}-{instanceID}.{region}.{dataPlaneDomain}
	sandboxDomain := fmt.Sprintf("%d-%s.%s.%s", 49999, instanceID, c.region, c.dataPlaneDomain)

	// Execute code via data plane
	return c.dataPlane.ExecuteCode(ctx, &ExecuteCodeConfig{
		Domain:      sandboxDomain,
		AccessToken: *tokenResp.Response.Token,
		Code:        codeStr,
		Language:    language,
	}, callbacks)
}

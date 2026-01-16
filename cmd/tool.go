package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/client"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

var (
	// tool create flags
	toolCreateName              string
	toolCreateType              string
	toolCreateDescription       string
	toolCreateTimeout           string
	toolCreateNetworkMode       string
	toolCreateTags              []string
	toolCreateRoleArn           string
	toolCreateMounts            []string
	toolCreateVPCSubnets        []string
	toolCreateVPCSecurityGroups []string

	// tool update flags
	toolUpdateDescription string
	toolUpdateNetworkMode string
	toolUpdateTags        []string
	toolUpdateClearTags   bool

	// tool list flags
	toolListIDs              []string
	toolListStatus           string
	toolListType             string
	toolListCreatedSince     string
	toolListCreatedSinceTime string
	toolListTags             []string
	toolListOffset           int
	toolListLimit            int
	toolListShort            bool
	toolListNoHeader         bool

	// tool common flags
	toolTime bool
)

// toolListCmd represents the tool list command
var toolListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List available tools",
	Long: `List sandbox tools with optional filtering and pagination.

Options:
  --id: Specific tool IDs to query (can be specified multiple times, max 100)

Filter options (ignored when --id is specified):
  --status: CREATING, ACTIVE, DELETING, FAILED
  --type: browser, code-interpreter, computer, mobile
  --created-since: Relative time, e.g., "5m", "1h", "24h"
  --created-since-time: Absolute time (RFC3339), e.g., "2024-01-15T10:30:00Z"
  --tag: Filter by tag (key=value format, can be specified multiple times)

Note: --created-since and --created-since-time cannot be used together.

Examples:
  ags tool list
  ags tool list --id tool-xxx --id tool-yyy
  ags tool list --status ACTIVE
  ags tool list --type code-interpreter --limit 10
  ags tool list --created-since 24h
  ags tool list --tag env=prod
  ags tool list --short
  ags tool list --no-header`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		start := time.Now()

		// Validate mutually exclusive options
		if toolListCreatedSince != "" && toolListCreatedSinceTime != "" {
			return fmt.Errorf("--created-since and --created-since-time cannot be used together")
		}

		// Parse tags
		tags := make(map[string]string)
		for _, tag := range toolListTags {
			parts := strings.SplitN(tag, "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid tag format: %s (expected key=value)", tag)
			}
			tags[parts[0]] = parts[1]
		}

		apiClient, err := client.NewClient(config.GetBackend())
		if err != nil {
			return fmt.Errorf("failed to create API client: %w", err)
		}

		result, err := apiClient.ListTools(ctx, &client.ListToolsOptions{
			ToolIDs:          toolListIDs,
			Status:           toolListStatus,
			ToolType:         toolListType,
			CreatedSince:     toolListCreatedSince,
			CreatedSinceTime: toolListCreatedSinceTime,
			Tags:             tags,
			Offset:           toolListOffset,
			Limit:            toolListLimit,
		})
		if err != nil {
			return fmt.Errorf("failed to list tools: %w", err)
		}

		totalDuration := time.Since(start)
		var timing *output.Timing
		if toolTime {
			timing = output.NewTiming(totalDuration)
		}

		f := output.NewFormatter()

		if len(result.Tools) == 0 {
			output.PrintInfo("No tools found")
			if toolTime && !f.IsJSON() {
				f.PrintTiming(timing)
			}
			return nil
		}

		var headers []string
		var rows [][]string

		if toolListShort {
			// Short format: only ID and NAME
			headers = []string{"ID", "NAME"}
			rows = make([][]string, len(result.Tools))
			for i, t := range result.Tools {
				rows[i] = []string{t.ID, t.Name}
			}
		} else {
			// Full format: ID, NAME, TYPE, NETWORK, MOUNTS, DESCRIPTION, TAGS, CREATED
			headers = []string{"ID", "NAME", "TYPE", "NETWORK", "MOUNTS", "DESCRIPTION", "TAGS", "CREATED"}
			rows = make([][]string, len(result.Tools))
			for i, t := range result.Tools {
				// Format tags as key=value pairs
				var tagStrs []string
				for k, v := range t.Tags {
					tagStrs = append(tagStrs, fmt.Sprintf("%s=%s", k, v))
				}
				tagsStr := strings.Join(tagStrs, ", ")
				mountsStr := client.FormatStorageMountSummary(t.StorageMounts)
				networkMode := t.NetworkMode
				if networkMode == "" {
					networkMode = "-"
				}
				createdAt := formatShortTime(t.CreatedAt)
				rows[i] = []string{t.ID, t.Name, t.Type, networkMode, mountsStr, output.TruncateString(t.Description, 40), tagsStr, createdAt}
			}
		}

		// Build pagination info
		var pagination *output.Pagination
		if result.TotalCount > 0 {
			pagination = &output.Pagination{
				Offset: toolListOffset,
				Limit:  toolListLimit,
				Total:  result.TotalCount,
			}
		}

		if toolListNoHeader {
			if err := f.PrintTableNoHeader(rows); err != nil {
				return err
			}
		} else {
			if err := f.PrintTable(headers, rows, pagination); err != nil {
				return err
			}
		}

		if toolTime && !f.IsJSON() {
			f.PrintTiming(timing)
		}

		return nil
	},
}

// toolGetCmd represents the tool get command
var toolGetCmd = &cobra.Command{
	Use:   "get <tool-id>",
	Short: "Get tool details",
	Long:  `Get detailed information about a specific tool.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		start := time.Now()
		toolID := args[0]

		apiClient, err := client.NewClient(config.GetBackend())
		if err != nil {
			return fmt.Errorf("failed to create API client: %w", err)
		}

		tool, err := apiClient.GetTool(ctx, toolID)
		if err != nil {
			return fmt.Errorf("failed to get tool: %w", err)
		}

		totalDuration := time.Since(start)
		var timing *output.Timing
		if toolTime {
			timing = output.NewTiming(totalDuration)
		}

		f := output.NewFormatter()

		// Format tags
		var tagStrs []string
		for k, v := range tool.Tags {
			tagStrs = append(tagStrs, fmt.Sprintf("%s=%s", k, v))
		}
		tagsStr := strings.Join(tagStrs, ", ")
		if tagsStr == "" {
			tagsStr = "-"
		}

		// Format storage mounts
		mountsStr := formatStorageMountsDetail(tool.StorageMounts)

		// Format network mode
		networkMode := tool.NetworkMode
		if networkMode == "" {
			networkMode = "-"
		}

		if f.IsJSON() {
			data := map[string]any{
				"id":          tool.ID,
				"name":        tool.Name,
				"type":        tool.Type,
				"networkMode": networkMode,
				"description": tool.Description,
				"tags":        tool.Tags,
				"createdAt":   tool.CreatedAt,
			}
			// Add VPC config if present
			if tool.NetworkMode == "VPC" && tool.VPCConfig != nil {
				data["vpcConfig"] = tool.VPCConfig
			}
			if tool.RoleArn != "" {
				data["roleArn"] = tool.RoleArn
			}
			if len(tool.StorageMounts) > 0 {
				data["storageMounts"] = tool.StorageMounts
			}
			if timing != nil {
				data["timing"] = timing
			}
			return f.PrintJSON(data)
		}

		// Build ordered output
		result := []output.KeyValue{
			{Key: "ID", Value: tool.ID},
			{Key: "Name", Value: tool.Name},
			{Key: "Type", Value: tool.Type},
			{Key: "NetworkMode", Value: networkMode},
		}

		// Add VPC config if present
		if tool.NetworkMode == "VPC" && tool.VPCConfig != nil {
			subnets, secGroups := client.FormatVPCConfigSummary(tool.VPCConfig)
			result = append(result,
				output.KeyValue{Key: "VPCSubnets", Value: subnets},
				output.KeyValue{Key: "VPCSecGroups", Value: secGroups},
			)
		}

		result = append(result,
			output.KeyValue{Key: "Description", Value: tool.Description},
			output.KeyValue{Key: "Tags", Value: tagsStr},
			output.KeyValue{Key: "Created", Value: formatShortTime(tool.CreatedAt)},
		)

		// Add RoleArn if present
		if tool.RoleArn != "" {
			result = append(result, output.KeyValue{Key: "RoleArn", Value: tool.RoleArn})
		}

		// Add StorageMounts if present
		if mountsStr != "" {
			result = append(result, output.KeyValue{Key: "StorageMounts", Value: mountsStr})
		}

		if err := f.PrintKeyValue(result); err != nil {
			return err
		}

		if toolTime {
			f.PrintTiming(timing)
		}

		return nil
	},
}

// formatStorageMountsDetail formats storage mounts for detailed display
func formatStorageMountsDetail(mounts []client.StorageMount) string {
	if len(mounts) == 0 {
		return ""
	}

	var lines []string
	for i, m := range mounts {
		lines = append(lines, fmt.Sprintf("\n  [%d] %s", i+1, m.Name))
		if m.StorageSource != nil && m.StorageSource.Cos != nil {
			lines = append(lines, "      Type:       cos")
			lines = append(lines, fmt.Sprintf("      Bucket:     %s", m.StorageSource.Cos.BucketName))
			lines = append(lines, fmt.Sprintf("      BucketPath: %s", m.StorageSource.Cos.BucketPath))
			if m.StorageSource.Cos.Endpoint != "" {
				lines = append(lines, fmt.Sprintf("      Endpoint:   %s", m.StorageSource.Cos.Endpoint))
			}
		}
		lines = append(lines, fmt.Sprintf("      MountPath:  %s", m.MountPath))
		lines = append(lines, fmt.Sprintf("      ReadOnly:   %t", m.ReadOnly))
	}
	return strings.Join(lines, "\n")
}

// formatShortTime formats ISO8601 time to short format (MM-DD HH:MM)
func formatShortTime(isoTime string) string {
	if isoTime == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, isoTime)
	if err != nil {
		return isoTime
	}
	return t.Local().Format("01-02 15:04")
}

func init() {
	addToolCommand(rootCmd)
}

// addToolCommand adds the tool command to a parent command
func addToolCommand(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:     "tool",
		Aliases: []string{"t"},
		Short:   "Manage sandbox tools",
		Long:    `Manage sandbox tools (templates). Tools define the type and capabilities of sandbox instances.`,
	}

	// tool create
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new sandbox tool",
		Long: `Create a new sandbox tool (template).

Tool types:
  - code-interpreter: Python code execution sandbox
  - browser: Browser automation sandbox

Network modes:
  - PUBLIC: Public network access (default)
  - VPC: VPC network (requires --vpc-subnet and --vpc-sg, cannot be changed after creation)
  - SANDBOX: No network / isolated environment
  - INTERNAL_SERVICE: Internal Tencent Cloud services only

Storage mount format (--mount):
  type=cos,name=<name>,bucket=<bucket>,src=<source-path>,dst=<target-path>[,readonly][,endpoint=<endpoint>]

Examples:
  ags tool create -n my-tool -t code-interpreter
  ags tool create -n my-browser -t browser -d "My browser tool" --timeout 10m
  ags tool create -n my-tool -t code-interpreter --network SANDBOX
  ags tool create -n my-tool -t code-interpreter --tag env=prod --tag team=ai
  ags tool create -n my-vpc-tool -t code-interpreter \
    --network VPC \
    --vpc-subnet subnet-xxx1 \
    --vpc-subnet subnet-xxx2 \
    --vpc-sg sg-yyy1
  ags tool create -n my-tool -t code-interpreter \
    --role-arn "qcs::cam::uin/100000:roleName/AGS_COS_Role" \
    --mount "type=cos,name=data,bucket=my-bucket-1250000000,src=/data,dst=/mnt/data"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			start := time.Now()

			if toolCreateName == "" {
				return fmt.Errorf("tool name is required (-n/--name)")
			}
			if toolCreateType == "" {
				return fmt.Errorf("tool type is required (-t/--type)")
			}
			if toolCreateType != "code-interpreter" && toolCreateType != "browser" {
				return fmt.Errorf("invalid tool type: %s (must be code-interpreter or browser)", toolCreateType)
			}

			// Validate network mode
			if toolCreateNetworkMode != "" {
				validModes := map[string]bool{"PUBLIC": true, "VPC": true, "SANDBOX": true, "INTERNAL_SERVICE": true}
				if !validModes[toolCreateNetworkMode] {
					return fmt.Errorf("invalid network mode: %s (must be PUBLIC, VPC, SANDBOX, or INTERNAL_SERVICE)", toolCreateNetworkMode)
				}
			}

			// Validate VPC configuration
			if toolCreateNetworkMode == "VPC" {
				if len(toolCreateVPCSubnets) == 0 {
					return fmt.Errorf("--vpc-subnet is required when --network=VPC")
				}
				if len(toolCreateVPCSecurityGroups) == 0 {
					return fmt.Errorf("--vpc-sg is required when --network=VPC")
				}
			} else if len(toolCreateVPCSubnets) > 0 || len(toolCreateVPCSecurityGroups) > 0 {
				return fmt.Errorf("--vpc-subnet and --vpc-sg can only be used with --network=VPC")
			}

			// Parse tags
			tags := make(map[string]string)
			for _, tag := range toolCreateTags {
				parts := strings.SplitN(tag, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("invalid tag format: %s (expected key=value)", tag)
				}
				tags[parts[0]] = parts[1]
			}

			// Parse storage mounts
			var storageMounts []client.StorageMount
			for _, mountStr := range toolCreateMounts {
				mount, err := client.ParseStorageMount(mountStr)
				if err != nil {
					return fmt.Errorf("invalid --mount: %w", err)
				}
				storageMounts = append(storageMounts, *mount)
			}

			// Validate: RoleArn is required when StorageMounts is set with COS
			if len(storageMounts) > 0 && toolCreateRoleArn == "" {
				return fmt.Errorf("--role-arn is required when --mount is specified")
			}

			// Build VPC config if needed
			var vpcConfig *client.VPCConfig
			if toolCreateNetworkMode == "VPC" {
				vpcConfig = &client.VPCConfig{
					SubnetIds:        toolCreateVPCSubnets,
					SecurityGroupIds: toolCreateVPCSecurityGroups,
				}
			}

			apiClient, err := client.NewClient(config.GetBackend())
			if err != nil {
				return fmt.Errorf("failed to create API client: %w", err)
			}

			tool, err := apiClient.CreateTool(ctx, &client.CreateToolOptions{
				Name:           toolCreateName,
				Type:           toolCreateType,
				Description:    toolCreateDescription,
				DefaultTimeout: toolCreateTimeout,
				NetworkMode:    toolCreateNetworkMode,
				VPCConfig:      vpcConfig,
				Tags:           tags,
				RoleArn:        toolCreateRoleArn,
				StorageMounts:  storageMounts,
			})
			if err != nil {
				return fmt.Errorf("failed to create tool: %w", err)
			}

			totalDuration := time.Since(start)
			var timing *output.Timing
			if toolTime {
				timing = output.NewTiming(totalDuration)
			}

			f := output.NewFormatter()

			if f.IsJSON() {
				data := map[string]any{
					"status":      "success",
					"message":     fmt.Sprintf("Tool created: %s", tool.ID),
					"id":          tool.ID,
					"name":        tool.Name,
					"type":        tool.Type,
					"networkMode": tool.NetworkMode,
					"description": tool.Description,
				}
				if tool.NetworkMode == "VPC" && tool.VPCConfig != nil {
					data["vpcConfig"] = tool.VPCConfig
				}
				if tool.RoleArn != "" {
					data["roleArn"] = tool.RoleArn
				}
				if len(tool.StorageMounts) > 0 {
					data["storageMounts"] = tool.StorageMounts
				}
				if timing != nil {
					data["timing"] = timing
				}
				return f.PrintJSON(data)
			}

			output.PrintSuccess(fmt.Sprintf("Tool created: %s", tool.ID))
			result := []output.KeyValue{
				{Key: "ID", Value: tool.ID},
				{Key: "Name", Value: tool.Name},
				{Key: "Type", Value: tool.Type},
				{Key: "NetworkMode", Value: tool.NetworkMode},
			}
			if tool.NetworkMode == "VPC" && tool.VPCConfig != nil {
				subnets, secGroups := client.FormatVPCConfigSummary(tool.VPCConfig)
				result = append(result,
					output.KeyValue{Key: "VPCSubnets", Value: subnets},
					output.KeyValue{Key: "VPCSecGroups", Value: secGroups},
				)
			}
			result = append(result, output.KeyValue{Key: "Description", Value: tool.Description})
			if tool.RoleArn != "" {
				result = append(result, output.KeyValue{Key: "RoleArn", Value: tool.RoleArn})
			}
			if len(tool.StorageMounts) > 0 {
				result = append(result, output.KeyValue{Key: "StorageMounts", Value: client.FormatStorageMountSummary(tool.StorageMounts)})
			}

			if err := f.PrintKeyValue(result); err != nil {
				return err
			}

			if toolTime {
				f.PrintTiming(timing)
			}

			return nil
		},
	}
	createCmd.Flags().StringVarP(&toolCreateName, "name", "n", "", "Tool name (required)")
	createCmd.Flags().StringVarP(&toolCreateType, "type", "t", "", "Tool type: code-interpreter or browser (required)")
	createCmd.Flags().StringVarP(&toolCreateDescription, "description", "d", "", "Tool description")
	createCmd.Flags().StringVar(&toolCreateTimeout, "timeout", "", "Default timeout (e.g., 5m, 300s, 1h)")
	createCmd.Flags().StringVar(&toolCreateNetworkMode, "network", "", "Network mode: PUBLIC (default), VPC, SANDBOX, INTERNAL_SERVICE")
	createCmd.Flags().StringArrayVar(&toolCreateVPCSubnets, "vpc-subnet", nil, "VPC subnet ID (can be specified multiple times, required when --network=VPC)")
	createCmd.Flags().StringArrayVar(&toolCreateVPCSecurityGroups, "vpc-sg", nil, "Security group ID (can be specified multiple times, required when --network=VPC)")
	createCmd.Flags().StringArrayVar(&toolCreateTags, "tag", nil, "Tags in key=value format (can be specified multiple times)")
	createCmd.Flags().StringVar(&toolCreateRoleArn, "role-arn", "", "Role ARN for COS access (required when --mount is specified)")
	createCmd.Flags().StringArrayVar(&toolCreateMounts, "mount", nil, "Storage mount config (can be specified multiple times)\n"+client.FormatStorageMountHelp())
	createCmd.Flags().BoolVar(&toolTime, "time", false, "Print elapsed time")
	cmd.AddCommand(createCmd)

	// tool list
	listCmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List available tools",
		Long:    toolListCmd.Long,
		RunE:    toolListCmd.RunE,
	}
	listCmd.Flags().StringArrayVar(&toolListIDs, "id", nil, "Specific tool IDs to query (can be specified multiple times)")
	listCmd.Flags().StringVar(&toolListStatus, "status", "", "Filter by status: CREATING, ACTIVE, DELETING, FAILED")
	listCmd.Flags().StringVar(&toolListType, "type", "", "Filter by type: browser, code-interpreter, computer, mobile")
	listCmd.Flags().StringVar(&toolListCreatedSince, "created-since", "", "Filter by relative time, e.g., 5m, 1h, 24h")
	listCmd.Flags().StringVar(&toolListCreatedSinceTime, "created-since-time", "", "Filter by absolute time (RFC3339)")
	listCmd.Flags().StringArrayVar(&toolListTags, "tag", nil, "Filter by tag (key=value, can be specified multiple times)")
	listCmd.Flags().IntVar(&toolListOffset, "offset", 0, "Pagination offset")
	listCmd.Flags().IntVar(&toolListLimit, "limit", 20, "Pagination limit (max 100)")
	listCmd.Flags().BoolVar(&toolListShort, "short", false, "Show only ID and NAME")
	listCmd.Flags().BoolVar(&toolListNoHeader, "no-header", false, "Hide table header and footer")
	listCmd.Flags().BoolVar(&toolTime, "time", false, "Print elapsed time")
	cmd.AddCommand(listCmd)

	// tool get
	getCmd := &cobra.Command{
		Use:   "get <tool-id>",
		Short: "Get tool details",
		Long:  `Get detailed information about a specific tool.`,
		Args:  cobra.ExactArgs(1),
		RunE:  toolGetCmd.RunE,
	}
	getCmd.Flags().BoolVar(&toolTime, "time", false, "Print elapsed time")
	cmd.AddCommand(getCmd)

	// tool update
	updateCmd := &cobra.Command{
		Use:   "update <tool-id>",
		Short: "Update a sandbox tool",
		Long: `Update a sandbox tool's description, network mode, or tags.

At least one of --description, --network, --tag, or --clear-tags must be specified.

Network modes:
  - PUBLIC: Public network access
  - SANDBOX: No network / isolated environment
  - INTERNAL_SERVICE: Internal Tencent Cloud services only

Note: VPC mode cannot be changed after creation. Tools created with VPC mode
cannot switch to other modes, and non-VPC tools cannot switch to VPC mode.

Examples:
  ags tool update sdt-xxx -d "Updated description"
  ags tool update sdt-xxx --network SANDBOX
  ags tool update sdt-xxx --tag env=staging --tag team=ai
  ags tool update sdt-xxx --clear-tags
  ags tool update sdt-xxx -d "New desc" --network PUBLIC --tag env=prod`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			start := time.Now()
			toolID := args[0]

			// Check if at least one update option is provided
			descriptionSet := cmd.Flags().Changed("description")
			networkSet := cmd.Flags().Changed("network")
			tagsSet := cmd.Flags().Changed("tag")

			if !descriptionSet && !networkSet && !tagsSet && !toolUpdateClearTags {
				return fmt.Errorf("at least one of --description, --network, --tag, or --clear-tags must be specified")
			}

			// Validate network mode if specified (VPC is not allowed for update)
			if networkSet {
				validModes := map[string]bool{"PUBLIC": true, "SANDBOX": true, "INTERNAL_SERVICE": true}
				if !validModes[toolUpdateNetworkMode] {
					if toolUpdateNetworkMode == "VPC" {
						return fmt.Errorf("cannot change network mode to VPC (VPC can only be set at creation time)")
					}
					return fmt.Errorf("invalid network mode: %s (must be PUBLIC, SANDBOX, or INTERNAL_SERVICE)", toolUpdateNetworkMode)
				}
			}

			// Build update options
			opts := &client.UpdateToolOptions{
				ToolID: toolID,
			}

			if descriptionSet {
				opts.Description = &toolUpdateDescription
			}

			if networkSet {
				opts.NetworkMode = &toolUpdateNetworkMode
			}

			// Handle tags
			if toolUpdateClearTags {
				// Clear all tags by setting empty map
				opts.Tags = make(map[string]string)
			} else if tagsSet {
				// Parse and set new tags
				tags := make(map[string]string)
				for _, tag := range toolUpdateTags {
					parts := strings.SplitN(tag, "=", 2)
					if len(parts) != 2 {
						return fmt.Errorf("invalid tag format: %s (expected key=value)", tag)
					}
					tags[parts[0]] = parts[1]
				}
				opts.Tags = tags
			}

			apiClient, err := client.NewClient(config.GetBackend())
			if err != nil {
				return fmt.Errorf("failed to create API client: %w", err)
			}

			if err := apiClient.UpdateTool(ctx, opts); err != nil {
				return fmt.Errorf("failed to update tool: %w", err)
			}

			totalDuration := time.Since(start)
			var timing *output.Timing
			if toolTime {
				timing = output.NewTiming(totalDuration)
			}

			f := output.NewFormatter()

			if f.IsJSON() {
				data := map[string]any{
					"status":  "success",
					"message": fmt.Sprintf("Tool updated: %s", toolID),
					"id":      toolID,
				}
				if timing != nil {
					data["timing"] = timing
				}
				return f.PrintJSON(data)
			}

			output.PrintSuccess(fmt.Sprintf("Tool updated: %s", toolID))

			if toolTime {
				f.PrintTiming(timing)
			}

			return nil
		},
	}
	updateCmd.Flags().StringVarP(&toolUpdateDescription, "description", "d", "", "Tool description")
	updateCmd.Flags().StringVar(&toolUpdateNetworkMode, "network", "", "Network mode: PUBLIC, SANDBOX, INTERNAL_SERVICE (VPC cannot be changed)")
	updateCmd.Flags().StringArrayVar(&toolUpdateTags, "tag", nil, "Tags in key=value format (can be specified multiple times)")
	updateCmd.Flags().BoolVar(&toolUpdateClearTags, "clear-tags", false, "Clear all tags")
	updateCmd.Flags().BoolVar(&toolTime, "time", false, "Print elapsed time")
	cmd.AddCommand(updateCmd)

	// tool delete
	deleteCmd := &cobra.Command{
		Use:     "delete <tool-id> [tool-id...]",
		Aliases: []string{"rm", "del"},
		Short:   "Delete sandbox tools",
		Long:    `Delete one or more sandbox tools by ID.`,
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			start := time.Now()

			apiClient, err := client.NewClient(config.GetBackend())
			if err != nil {
				return fmt.Errorf("failed to create API client: %w", err)
			}

			f := output.NewFormatter()
			var failed []string

			for _, toolID := range args {
				if err := apiClient.DeleteTool(ctx, toolID); err != nil {
					output.PrintWarning(fmt.Sprintf("Failed to delete tool %s: %v", toolID, err))
					failed = append(failed, toolID)
				} else {
					if !f.IsJSON() {
						output.PrintSuccess(fmt.Sprintf("Tool deleted: %s", toolID))
					}
				}
			}

			totalDuration := time.Since(start)
			var timing *output.Timing
			if toolTime {
				timing = output.NewTiming(totalDuration)
			}

			if f.IsJSON() {
				data := map[string]any{
					"status":  "success",
					"deleted": len(args) - len(failed),
					"failed":  len(failed),
				}
				if len(failed) > 0 {
					data["status"] = "partial"
					data["failed_ids"] = failed
				}
				if timing != nil {
					data["timing"] = timing
				}
				return f.PrintJSON(data)
			}

			if toolTime {
				f.PrintTiming(timing)
			}

			if len(failed) > 0 {
				return fmt.Errorf("failed to delete %d tool(s)", len(failed))
			}
			return nil
		},
	}
	deleteCmd.Flags().BoolVar(&toolTime, "time", false, "Print elapsed time")
	cmd.AddCommand(deleteCmd)

	parent.AddCommand(cmd)
}

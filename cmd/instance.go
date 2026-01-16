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
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/utils"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/webshell"
)

var (
	instanceTool         string
	instanceToolID       string
	instanceTimeout      int
	instanceTime         bool
	instanceMountOptions []string

	// list command flags
	instanceListTool     string
	instanceListStatus   string
	instanceListShort    bool
	instanceListNoHeader bool
	instanceListOffset   int
	instanceListLimit    int

	// login command flags
	instanceLoginNoBrowser  bool
	instanceLoginTTYDBinary string
)

// instanceCreateCmd represents the instance create command
var instanceCreateCmd = &cobra.Command{
	Use:     "create",
	Aliases: []string{"c"},
	Short:   "Create a new instance",
	Long: `Create a new sandbox instance from a tool template.

Use --tool-name/-t for tool name (e2b/cloud backend) or --tool-id for tool ID (cloud backend only).

Mount option format (--mount-option):
  name=<name>[,dst=<target-path>][,subpath=<sub-path>][,readonly]

Examples:
  ags instance create -t code-interpreter-v1
  ags instance create --tool-name code-interpreter-v1
  ags instance create --tool code-interpreter-v1
  ags instance create --tool-id sdt-xxxx
  ags instance create -t my-tool --timeout 600
  ags instance create --tool-id sdt-xxxx --mount-option "name=data,dst=/workspace,subpath=user-123"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		start := time.Now()

		// Validate tool parameters
		if instanceTool != "" && instanceToolID != "" {
			return fmt.Errorf("cannot specify both --tool-name/--tool and --tool-id")
		}
		if instanceTool == "" && instanceToolID == "" {
			return fmt.Errorf("must specify either --tool-name/--tool or --tool-id")
		}

		if err := config.Validate(); err != nil {
			return err
		}

		// Parse mount options
		var mountOptions []client.MountOption
		for _, optStr := range instanceMountOptions {
			opt, err := client.ParseMountOption(optStr)
			if err != nil {
				return fmt.Errorf("invalid --mount-option: %w", err)
			}
			mountOptions = append(mountOptions, *opt)
		}

		apiClient, err := client.NewClient(config.GetBackend())
		if err != nil {
			return fmt.Errorf("failed to create API client: %w", err)
		}

		opts := &client.CreateInstanceOptions{
			ToolID:       instanceToolID,
			ToolName:     instanceTool,
			Timeout:      instanceTimeout,
			MountOptions: mountOptions,
		}

		instance, err := apiClient.CreateInstance(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to create instance: %w", err)
		}

		totalDuration := time.Since(start)
		var timing *output.Timing
		if instanceTime {
			timing = output.NewTiming(totalDuration)
		}

		f := output.NewFormatter()

		if f.IsJSON() {
			data := map[string]any{
				"status":         "success",
				"message":        fmt.Sprintf("Instance created: %s", instance.ID),
				"id":             instance.ID,
				"tool":           instance.ToolName,
				"toolId":         instance.ToolID,
				"instanceStatus": instance.Status,
				"createdAt":      instance.CreatedAt,
			}
			if len(instance.MountOptions) > 0 {
				data["mountOptions"] = instance.MountOptions
			}
			if timing != nil {
				data["timing"] = timing
			}
			return f.PrintJSON(data)
		}

		output.PrintSuccess(fmt.Sprintf("Instance created: %s", instance.ID))

		result := []output.KeyValue{
			{Key: "ID", Value: instance.ID},
			{Key: "Tool", Value: instance.ToolName},
			{Key: "Status", Value: instance.Status},
			{Key: "Created", Value: instance.CreatedAt},
		}

		// Add mount options if present
		if len(instance.MountOptions) > 0 {
			result = append(result, output.KeyValue{Key: "MountOptions", Value: formatMountOptionsSummary(instance.MountOptions)})
		}

		if err := f.PrintKeyValue(result); err != nil {
			return err
		}

		if instanceTime {
			f.PrintTiming(timing)
		}

		return nil
	},
}

// formatMountOptionsSummary formats mount options for display
func formatMountOptionsSummary(opts []client.MountOption) string {
	if len(opts) == 0 {
		return "-"
	}
	var parts []string
	for _, opt := range opts {
		parts = append(parts, opt.Name)
	}
	return strings.Join(parts, ", ")
}

// formatMountOptionsDetail formats mount options for detailed display
func formatMountOptionsDetail(opts []client.MountOption) string {
	if len(opts) == 0 {
		return ""
	}

	var lines []string
	for i, opt := range opts {
		lines = append(lines, fmt.Sprintf("\n  [%d] %s", i+1, opt.Name))
		lines = append(lines, fmt.Sprintf("      MountPath: %s", valueOrDefault(opt.MountPath, "(default)")))
		if opt.SubPath != "" {
			lines = append(lines, fmt.Sprintf("      SubPath:   %s", opt.SubPath))
		}
		readOnly := "(default)"
		if opt.ReadOnly != nil {
			readOnly = fmt.Sprintf("%t", *opt.ReadOnly)
		}
		lines = append(lines, fmt.Sprintf("      ReadOnly:  %s", readOnly))
	}
	return strings.Join(lines, "\n")
}

// valueOrDefault returns the value if non-empty, otherwise the default
func valueOrDefault(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

// instanceListCmd represents the instance list command
var instanceListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List instances",
	Long: `List sandbox instances with optional filters.

Examples:
  ags instance list
  ags instance list --tool-id sdt-xxx
  ags instance list --status RUNNING
  ags instance list --short
  ags instance list --no-header
  ags instance list --offset 0 --limit 50`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		start := time.Now()

		if err := config.Validate(); err != nil {
			return err
		}

		apiClient, err := client.NewClient(config.GetBackend())
		if err != nil {
			return fmt.Errorf("failed to create API client: %w", err)
		}

		opts := &client.ListInstancesOptions{
			ToolID: instanceListTool,
			Status: instanceListStatus,
			Offset: instanceListOffset,
			Limit:  instanceListLimit,
		}

		result, err := apiClient.ListInstances(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to list instances: %w", err)
		}

		totalDuration := time.Since(start)
		var timing *output.Timing
		if instanceTime {
			timing = output.NewTiming(totalDuration)
		}

		f := output.NewFormatter()

		if len(result.Instances) == 0 {
			output.PrintInfo("No instances found")
			if instanceTime && !f.IsJSON() {
				f.PrintTiming(timing)
			}
			return nil
		}

		if instanceListShort {
			// Short format: only ID
			if f.IsJSON() {
				ids := make([]string, len(result.Instances))
				for i, inst := range result.Instances {
					ids[i] = inst.ID
				}
				data := map[string]any{"ids": ids}
				if timing != nil {
					data["timing"] = timing
				}
				return f.PrintJSON(data)
			}
			for _, inst := range result.Instances {
				fmt.Println(inst.ID)
			}
			if instanceTime {
				f.PrintTiming(timing)
			}
			return nil
		}

		headers := []string{"ID", "TOOL", "STATUS", "TIMEOUT", "EXPIRES", "MOUNTS", "CREATED"}
		rows := make([][]string, len(result.Instances))
		for i, inst := range result.Instances {
			timeout := "-"
			if inst.TimeoutSeconds != nil {
				timeout = formatTimeout(*inst.TimeoutSeconds)
			}
			expires := "-"
			if inst.ExpiresAt != "" {
				expires = formatTimeShort(inst.ExpiresAt)
			}
			mounts := formatMountOptionsSummary(inst.MountOptions)
			rows[i] = []string{
				inst.ID,
				inst.ToolName,
				inst.Status,
				timeout,
				expires,
				mounts,
				formatTimeShort(inst.CreatedAt),
			}
		}

		// Build pagination info
		var pagination *output.Pagination
		if result.TotalCount > 0 {
			pagination = &output.Pagination{
				Offset: instanceListOffset,
				Limit:  instanceListLimit,
				Total:  result.TotalCount,
			}
		}

		if instanceListNoHeader {
			if err := f.PrintTableNoHeader(rows); err != nil {
				return err
			}
		} else {
			if err := f.PrintTable(headers, rows, pagination); err != nil {
				return err
			}
		}

		if instanceTime && !f.IsJSON() {
			f.PrintTiming(timing)
		}

		return nil
	},
}

// formatTimeout formats timeout seconds to human readable format
func formatTimeout(seconds uint64) string {
	if seconds >= 3600 && seconds%3600 == 0 {
		return fmt.Sprintf("%dh", seconds/3600)
	}
	if seconds >= 60 && seconds%60 == 0 {
		return fmt.Sprintf("%dm", seconds/60)
	}
	return fmt.Sprintf("%ds", seconds)
}

// formatTimeShort formats ISO8601 time to short format
func formatTimeShort(isoTime string) string {
	if isoTime == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, isoTime)
	if err != nil {
		return isoTime
	}
	return t.Local().Format("01-02 15:04")
}

// instanceGetCmd represents the instance get command
var instanceGetCmd = &cobra.Command{
	Use:   "get <instance-id>",
	Short: "Get instance details",
	Long:  `Get detailed information about a specific instance.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		start := time.Now()
		instanceID := args[0]

		if err := config.Validate(); err != nil {
			return err
		}

		apiClient, err := client.NewClient(config.GetBackend())
		if err != nil {
			return fmt.Errorf("failed to create API client: %w", err)
		}

		instance, err := apiClient.GetInstance(ctx, instanceID)
		if err != nil {
			return fmt.Errorf("failed to get instance: %w", err)
		}

		totalDuration := time.Since(start)
		var timing *output.Timing
		if instanceTime {
			timing = output.NewTiming(totalDuration)
		}

		f := output.NewFormatter()

		if f.IsJSON() {
			data := map[string]any{
				"id":        instance.ID,
				"toolId":    instance.ToolID,
				"toolName":  instance.ToolName,
				"status":    instance.Status,
				"createdAt": instance.CreatedAt,
			}
			if instance.UpdatedAt != "" {
				data["updatedAt"] = instance.UpdatedAt
			}
			if instance.TimeoutSeconds != nil {
				data["timeoutSeconds"] = *instance.TimeoutSeconds
			}
			if instance.ExpiresAt != "" {
				data["expiresAt"] = instance.ExpiresAt
			}
			if instance.StopReason != "" {
				data["stopReason"] = instance.StopReason
			}
			if len(instance.Endpoints) > 0 {
				data["endpoints"] = instance.Endpoints
			}
			if len(instance.MountOptions) > 0 {
				data["mountOptions"] = instance.MountOptions
			}
			if timing != nil {
				data["timing"] = timing
			}
			return f.PrintJSON(data)
		}

		// Build ordered key-value pairs
		result := []output.KeyValue{
			{Key: "ID", Value: instance.ID},
			{Key: "ToolID", Value: instance.ToolID},
			{Key: "ToolName", Value: instance.ToolName},
			{Key: "Status", Value: instance.Status},
			{Key: "Created", Value: instance.CreatedAt},
		}

		if instance.UpdatedAt != "" {
			result = append(result, output.KeyValue{Key: "Updated", Value: instance.UpdatedAt})
		}

		if instance.TimeoutSeconds != nil {
			result = append(result, output.KeyValue{Key: "Timeout", Value: formatTimeout(*instance.TimeoutSeconds)})
		}

		if instance.ExpiresAt != "" {
			result = append(result, output.KeyValue{Key: "Expires", Value: instance.ExpiresAt})
		}

		if instance.StopReason != "" {
			result = append(result, output.KeyValue{Key: "StopReason", Value: instance.StopReason})
		}

		// Add endpoints if present
		if len(instance.Endpoints) > 0 {
			result = append(result, output.KeyValue{Key: "Endpoints", Value: formatEndpoints(instance.Endpoints)})
		}

		// Add mount options if present
		mountOptsStr := formatMountOptionsDetail(instance.MountOptions)
		if mountOptsStr != "" {
			result = append(result, output.KeyValue{Key: "MountOptions", Value: mountOptsStr})
		}

		if err := f.PrintKeyValue(result); err != nil {
			return err
		}

		if instanceTime {
			f.PrintTiming(timing)
		}

		return nil
	},
}

// formatEndpoints formats endpoints for display
func formatEndpoints(endpoints []client.Endpoint) string {
	if len(endpoints) == 0 {
		return "-"
	}
	var parts []string
	for _, ep := range endpoints {
		parts = append(parts, fmt.Sprintf("%s (%s)", ep.URL, ep.Scope))
	}
	return strings.Join(parts, "\n")
}

// instanceDeleteCmd represents the instance delete command
var instanceDeleteCmd = &cobra.Command{
	Use:     "delete <instance-id> [instance-id...]",
	Aliases: []string{"rm", "del"},
	Short:   "Delete instances",
	Long:    `Delete one or more sandbox instances.`,
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		start := time.Now()

		if err := config.Validate(); err != nil {
			return err
		}

		apiClient, err := client.NewClient(config.GetBackend())
		if err != nil {
			return fmt.Errorf("failed to create API client: %w", err)
		}

		f := output.NewFormatter()
		var failed []string

		for _, instanceID := range args {
			if err := apiClient.DeleteInstance(ctx, instanceID); err != nil {
				output.PrintWarning(fmt.Sprintf("Failed to delete instance %s: %v", instanceID, err))
				failed = append(failed, instanceID)
			} else {
				if !f.IsJSON() {
					output.PrintSuccess(fmt.Sprintf("Instance deleted: %s", instanceID))
				}
			}
		}

		totalDuration := time.Since(start)
		var timing *output.Timing
		if instanceTime {
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

		if instanceTime {
			f.PrintTiming(timing)
		}

		if len(failed) > 0 {
			return fmt.Errorf("failed to delete %d instance(s)", len(failed))
		}
		return nil
	},
}

// instanceLoginCmd represents the instance login command
var instanceLoginCmd = &cobra.Command{
	Use:   "login <instance-id>",
	Short: "Login to instance via webshell",
	Long: `Login to a sandbox instance via web-based terminal (webshell).

This command will:
1. Verify the instance exists and is running
2. Download and start ttyd webshell service if not already running
   (or upload custom ttyd binary if --ttyd-binary is specified)
3. Open the webshell in your default browser

The webshell provides a full terminal interface accessible through your browser,
allowing you to interact with the sandbox environment directly.

If the sandbox cannot download ttyd from GitHub due to network restrictions,
you can use --ttyd-binary to upload a local ttyd binary file.

Examples:
  ags instance login abc123
  ags instance login abc123 --no-browser
  ags instance login abc123 --ttyd-binary /path/to/ttyd`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		start := time.Now()
		instanceID := args[0]

		if err := config.Validate(); err != nil {
			return err
		}

		apiClient, err := client.NewClient(config.GetBackend())
		if err != nil {
			return fmt.Errorf("failed to create API client: %w", err)
		}

		// Get instance information
		output.PrintInfo(fmt.Sprintf("Connecting to instance %s...", instanceID))
		instance, err := apiClient.GetInstance(ctx, instanceID)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				return fmt.Errorf("instance %s not found. Please check the instance ID and try again", instanceID)
			}
			if strings.Contains(err.Error(), "permission") || strings.Contains(err.Error(), "access") {
				return fmt.Errorf("access denied to instance %s. Please check your permissions", instanceID)
			}
			return fmt.Errorf("failed to get instance %s: %w", instanceID, err)
		}

		// Check instance status
		if instance.Status != "RUNNING" {
			switch instance.Status {
			case "CREATING":
				return fmt.Errorf("instance %s is still being created. Please wait for it to finish and try again", instanceID)
			case "STOPPED", "STOPPING":
				return fmt.Errorf("instance %s is stopped. Please start it first using 'ags instance create' or contact support", instanceID)
			case "ERROR", "FAILED":
				return fmt.Errorf("instance %s is in error state. Please contact support or create a new instance", instanceID)
			default:
				return fmt.Errorf("instance %s is not running (status: %s). Please wait for it to be ready", instanceID, instance.Status)
			}
		}

		// Check if instance type supports webshell
		supportedTypes := []string{"code-interpreter", "browser"}
		toolType := strings.ToLower(instance.ToolName)
		supported := false
		for _, t := range supportedTypes {
			if strings.Contains(toolType, t) {
				supported = true
				break
			}
		}
		if !supported {
			return fmt.Errorf("webshell is not supported for tool type '%s'.\nSupported types: %v\nPlease use a supported tool type or try other access methods", instance.ToolName, supportedTypes)
		}

		// Create webshell manager with connect options
		webshellMgr := webshell.NewManager(getConnectOptions()...)

		// Acquire access token for the instance (same as browser vnc command)
		accessToken, err := acquireInstanceToken(ctx, instanceID)
		if err != nil {
			return fmt.Errorf("failed to acquire access token: %w", err)
		}

		output.PrintInfo("Checking webshell status...")

		// Check if ttyd is already running
		running, err := webshellMgr.IsRunning(ctx, instanceID)
		if err != nil {
			output.PrintWarning("Failed to check webshell status, will attempt to start service")
			running = false // Assume not running, try to start
		}

		if !running {
			output.PrintInfo("Setting up webshell service...")
			output.PrintInfo("This may take a few moments on first use...")

			// Download or upload ttyd
			if instanceLoginTTYDBinary != "" {
				// Upload custom ttyd binary
				output.PrintInfo(fmt.Sprintf("Uploading custom ttyd binary from %s...", instanceLoginTTYDBinary))
				if err := webshellMgr.UploadTTYD(ctx, instanceID, instanceLoginTTYDBinary); err != nil {
					if strings.Contains(err.Error(), "validation failed") {
						return fmt.Errorf("invalid ttyd binary file: %w\n\nTip: Please ensure the file is a valid ttyd binary for the target architecture", err)
					}
					if strings.Contains(err.Error(), "does not exist") {
						return fmt.Errorf("ttyd binary file not found: %w\n\nTip: Please check the file path and try again", err)
					}
					return fmt.Errorf("failed to upload ttyd binary: %w", err)
				}
				output.PrintSuccess("Custom ttyd binary uploaded successfully")
			} else {
				// Download ttyd from GitHub
				if err := webshellMgr.Download(ctx, instanceID); err != nil {
					if strings.Contains(err.Error(), "unsupported platform") {
						return fmt.Errorf("webshell is not supported on this platform: %w", err)
					}
					if strings.Contains(err.Error(), "download timeout") || strings.Contains(err.Error(), "network") {
						return fmt.Errorf("failed to download webshell service due to network issues. Please check your connection and try again, or use --ttyd-binary to upload a local ttyd binary: %w", err)
					}
					return fmt.Errorf("failed to download webshell service: %w\n\nTip: This might be a temporary network issue. Please try again in a few moments, or use --ttyd-binary to upload a local ttyd binary", err)
				}
			}

			// Start ttyd service
			if err := webshellMgr.Start(ctx, instanceID, accessToken); err != nil {
				if strings.Contains(err.Error(), "port.*already in use") {
					return fmt.Errorf("webshell port is already in use. Another webshell session might be running.\nPlease wait a moment and try again, or contact support if the issue persists")
				}
				if strings.Contains(err.Error(), "health check failed") {
					return fmt.Errorf("webshell service failed to start properly: %w\n\nTip: This might be a temporary issue. Please try again in a few moments", err)
				}
				return fmt.Errorf("failed to start webshell service: %w\n\nTip: Please try again in a few moments. If the issue persists, contact support", err)
			}

			output.PrintSuccess("Webshell service started successfully")
		} else {
			output.PrintInfo("Webshell service is already running")
		}

		// Build access URL
		webshellURL := buildWebshellURL(instanceID, accessToken)

		totalDuration := time.Since(start)
		var timing *output.Timing
		if instanceTime {
			timing = output.NewTiming(totalDuration)
		}

		f := output.NewFormatter()

		if f.IsJSON() {
			data := map[string]any{
				"status":      "success",
				"message":     "Webshell is ready",
				"instanceId":  instanceID,
				"webshellUrl": webshellURL,
				"toolName":    instance.ToolName,
			}
			if timing != nil {
				data["timing"] = timing
			}
			return f.PrintJSON(data)
		}

		// Print access information
		result := []output.KeyValue{
			{Key: "Instance", Value: instanceID},
			{Key: "Tool", Value: instance.ToolName},
			{Key: "Webshell URL", Value: webshellURL},
		}

		if err := f.PrintKeyValue(result); err != nil {
			return err
		}

		// Try to open browser
		if !instanceLoginNoBrowser {
			if utils.IsBrowserAvailable() {
				output.PrintInfo("Opening webshell in browser...")
				if err := utils.OpenBrowser(webshellURL); err != nil {
					output.PrintWarning(fmt.Sprintf("Failed to open browser automatically: %v", err))
					output.PrintInfo("Please manually copy and paste the URL above into your browser")
					output.PrintInfo("Tip: You can use --no-browser flag to skip automatic browser opening")
				} else {
					output.PrintSuccess("Webshell opened in browser successfully")
					output.PrintInfo("You should now see the terminal interface in your browser")
					output.PrintInfo("Tip: If the page doesn't load, wait a moment and refresh")
				}
			} else {
				output.PrintWarning("No browser available on this system")
				output.PrintInfo("Please manually copy and paste the URL above into a browser on any device")
				output.PrintInfo("The webshell will be accessible from any browser with network access to the instance")
			}
		} else {
			output.PrintInfo("Browser opening disabled. Please manually open the URL above")
		}

		if instanceTime {
			f.PrintTiming(timing)
		}

		return nil
	},
}

// buildWebshellURL builds webshell access URL
// Format: https://{port}-{instance_id}.{region}.{domain}/?access_token={token}
func buildWebshellURL(instanceID, accessToken string) string {
	cloudCfg := config.GetCloudConfig()
	host := fmt.Sprintf("8080-%s.%s.%s", instanceID, cloudCfg.Region, cloudCfg.DataPlaneDomain())
	return fmt.Sprintf("https://%s/?access_token=%s", host, accessToken)
}

func init() {
	addInstanceCommand(rootCmd)
}

// addInstanceCommand adds the instance command to a parent command
func addInstanceCommand(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:     "instance",
		Aliases: []string{"i"},
		Short:   "Manage sandbox instances",
		Long:    `Manage sandbox instances. Instances are running sandboxes created from tools.`,
	}

	createCmd := &cobra.Command{
		Use:     "create",
		Aliases: []string{"c"},
		Short:   "Create/start a new instance",
		Long:    instanceCreateCmd.Long,
		RunE:    instanceCreateCmd.RunE,
	}
	createCmd.Flags().StringVarP(&instanceTool, "tool-name", "t", "", "Tool name (e2b/cloud backend)")
	createCmd.Flags().StringVar(&instanceTool, "tool", "", "Tool name (alias for --tool-name)")
	createCmd.Flags().StringVar(&instanceToolID, "tool-id", "", "Tool ID (cloud backend only)")
	createCmd.Flags().IntVar(&instanceTimeout, "timeout", 300, "Instance timeout in seconds")
	createCmd.Flags().BoolVar(&instanceTime, "time", false, "Print elapsed time to stderr")
	createCmd.Flags().StringArrayVar(&instanceMountOptions, "mount-option", nil, "Mount option to override tool storage config\n"+client.FormatMountOptionHelp())
	cmd.AddCommand(createCmd)

	// start is an alias for create, but shown as separate command
	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start a new instance (alias for create)",
		Long:  `Start a new sandbox instance from a tool template. This is an alias for 'create'.`,
		RunE:  instanceCreateCmd.RunE,
	}
	startCmd.Flags().StringVarP(&instanceTool, "tool-name", "t", "", "Tool name (e2b/cloud backend)")
	startCmd.Flags().StringVar(&instanceTool, "tool", "", "Tool name (alias for --tool-name)")
	startCmd.Flags().StringVar(&instanceToolID, "tool-id", "", "Tool ID (cloud backend only)")
	startCmd.Flags().IntVar(&instanceTimeout, "timeout", 300, "Instance timeout in seconds")
	startCmd.Flags().BoolVar(&instanceTime, "time", false, "Print elapsed time to stderr")
	startCmd.Flags().StringArrayVar(&instanceMountOptions, "mount-option", nil, "Mount option to override tool storage config\n"+client.FormatMountOptionHelp())
	cmd.AddCommand(startCmd)

	listCmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List instances",
		Long:    instanceListCmd.Long,
		RunE:    instanceListCmd.RunE,
	}
	listCmd.Flags().StringVar(&instanceListTool, "tool-id", "", "Filter by tool ID")
	listCmd.Flags().StringVarP(&instanceListStatus, "status", "s", "", "Filter by status (STARTING, RUNNING, FAILED, STOPPING, STOPPED)")
	listCmd.Flags().BoolVar(&instanceListShort, "short", false, "Only show instance IDs")
	listCmd.Flags().BoolVar(&instanceListNoHeader, "no-header", false, "Hide table header")
	listCmd.Flags().IntVar(&instanceListOffset, "offset", 0, "Pagination offset")
	listCmd.Flags().IntVar(&instanceListLimit, "limit", 20, "Pagination limit (max 100)")
	listCmd.Flags().BoolVar(&instanceTime, "time", false, "Print elapsed time")
	cmd.AddCommand(listCmd)

	getCmd := &cobra.Command{
		Use:   "get <instance-id>",
		Short: "Get instance details",
		Long:  `Get detailed information about a specific instance.`,
		Args:  cobra.ExactArgs(1),
		RunE:  instanceGetCmd.RunE,
	}
	getCmd.Flags().BoolVar(&instanceTime, "time", false, "Print elapsed time")
	cmd.AddCommand(getCmd)

	deleteCmd := &cobra.Command{
		Use:     "delete <instance-id> [instance-id...]",
		Aliases: []string{"rm", "del"},
		Short:   "Delete instances",
		Long:    `Delete one or more sandbox instances.`,
		Args:    cobra.MinimumNArgs(1),
		RunE:    instanceDeleteCmd.RunE,
	}
	deleteCmd.Flags().BoolVar(&instanceTime, "time", false, "Print elapsed time")
	cmd.AddCommand(deleteCmd)

	// stop is an alias for delete, but shown as separate command
	stopCmd := &cobra.Command{
		Use:   "stop <instance-id> [instance-id...]",
		Short: "Stop instances (alias for delete)",
		Long:  `Stop one or more sandbox instances. This is an alias for 'delete'.`,
		Args:  cobra.MinimumNArgs(1),
		RunE:  instanceDeleteCmd.RunE,
	}
	stopCmd.Flags().BoolVar(&instanceTime, "time", false, "Print elapsed time")
	cmd.AddCommand(stopCmd)

	// login command
	loginCmd := &cobra.Command{
		Use:   "login <instance-id>",
		Short: "Login to instance via webshell",
		Long:  instanceLoginCmd.Long,
		Args:  cobra.ExactArgs(1),
		RunE:  instanceLoginCmd.RunE,
	}
	loginCmd.Flags().BoolVar(&instanceLoginNoBrowser, "no-browser", false, "Don't open browser automatically")
	loginCmd.Flags().StringVar(&instanceLoginTTYDBinary, "ttyd-binary", "", "Path to custom ttyd binary file to upload")
	loginCmd.Flags().BoolVar(&instanceTime, "time", false, "Print elapsed time")
	cmd.AddCommand(loginCmd)

	parent.AddCommand(cmd)
}

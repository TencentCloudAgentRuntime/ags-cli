package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/client"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

var (
	// browser command flags
	browserInstance string
	browserTool     string
	browserToolID   string
	browserTimeout  int
	browserTime     bool
	browserPort     int
)

func init() {
	addBrowserCommand(rootCmd)
}

// addBrowserCommand adds the browser command to a parent command
func addBrowserCommand(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:     "browser",
		Aliases: []string{"b"},
		Short:   "Manage browser sandbox",
		Long: `Manage browser sandbox instances.

Browser sandboxes provide a remote browser environment accessible via VNC.`,
	}

	// vnc subcommand - show VNC URL
	vncCmd := &cobra.Command{
		Use:   "vnc",
		Short: "Show VNC URL for browser sandbox",
		Long: `Show the VNC URL for accessing a browser sandbox.

You can either connect to an existing instance or create a new one.
Use --tool-name/-t for tool name or --tool-id for tool ID (cloud backend only).

Examples:
  # Show VNC URL for existing instance
  ags browser vnc --instance <id>
  ags browser vnc -i <id>

  # Create new browser sandbox and show VNC URL
  ags browser vnc --tool-name browser-v1
  ags browser vnc -t browser-v1
  ags browser vnc --tool browser-v1
  ags browser vnc --tool-id sdt-xxxx

  # Create with custom timeout (1 hour)
  ags browser vnc --tool-name browser-v1 --timeout 3600`,
		RunE: browserVNCCommand,
	}

	vncCmd.Flags().StringVarP(&browserInstance, "instance", "i", "", "Instance ID to connect to")
	vncCmd.Flags().StringVarP(&browserTool, "tool-name", "t", "", "Tool name for creating new instance")
	vncCmd.Flags().StringVar(&browserTool, "tool", "", "Tool name for creating new instance (alias for --tool-name)")
	vncCmd.Flags().StringVar(&browserToolID, "tool-id", "", "Tool ID (cloud backend only)")
	vncCmd.Flags().IntVar(&browserTimeout, "timeout", 300, "Instance timeout in seconds")
	vncCmd.Flags().BoolVar(&browserTime, "time", false, "Print elapsed time")
	vncCmd.Flags().IntVarP(&browserPort, "port", "p", 9000, "VNC service port")

	cmd.AddCommand(vncCmd)
	parent.AddCommand(cmd)
}

func browserVNCCommand(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	start := time.Now()

	if err := config.Validate(); err != nil {
		return err
	}

	// Validate parameters
	if browserInstance != "" && (browserTool != "" || browserToolID != "") {
		return fmt.Errorf("cannot specify both --instance and tool parameters")
	}
	if browserInstance == "" && browserTool == "" && browserToolID == "" {
		return fmt.Errorf("must specify either --instance or tool parameters (--tool-name/--tool or --tool-id)")
	}
	if browserTool != "" && browserToolID != "" {
		return fmt.Errorf("cannot specify both --tool-name/--tool and --tool-id")
	}

	apiClient, err := client.NewClient(config.GetBackend())
	if err != nil {
		return fmt.Errorf("failed to create API client: %w", err)
	}

	var instance *client.Instance

	if browserInstance != "" {
		// Get existing instance
		instance, err = apiClient.GetInstance(ctx, browserInstance)
		if err != nil {
			return fmt.Errorf("failed to get instance: %w", err)
		}
	} else {
		// Create new instance
		opts := &client.CreateInstanceOptions{
			ToolName: browserTool,
			ToolID:   browserToolID,
			Timeout:  browserTimeout,
		}

		instance, err = apiClient.CreateInstance(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to create instance: %w", err)
		}

		output.PrintInfo(fmt.Sprintf("Created browser instance: %s", instance.ID))
	}

	// Acquire access token for the instance
	accessToken, err := acquireInstanceToken(ctx, instance.ID)
	if err != nil {
		return fmt.Errorf("failed to acquire access token: %w", err)
	}

	// Get cloud config for region info
	cloudCfg := config.GetCloudConfig()

	// Build VNC URL
	// Format: https://{port}-{sandbox_id}.{region}.{domain}/novnc/vnc_lite.html?&path=websockify?access_token={token}
	vncURL := buildVNCURL(instance.ID, cloudCfg.Region, cloudCfg.DataPlaneDomain(), accessToken, browserPort)

	// Build CDP URL for programmatic access
	cdpURL := buildCDPURL(instance.ID, cloudCfg.Region, cloudCfg.DataPlaneDomain(), accessToken, browserPort)

	totalDuration := time.Since(start)
	var timing *output.Timing
	if browserTime {
		timing = output.NewTiming(totalDuration)
	}

	f := output.NewFormatter()

	if f.IsJSON() {
		data := map[string]any{
			"instance_id":  instance.ID,
			"tool":         instance.ToolName,
			"status":       instance.Status,
			"vnc_url":      vncURL,
			"cdp_url":      cdpURL,
			"access_token": accessToken,
		}
		if browserTime {
			data["duration_ms"] = totalDuration.Milliseconds()
		}
		return f.PrintJSON(data)
	}

	// Text output
	result := []output.KeyValue{
		{Key: "Instance ID", Value: instance.ID},
		{Key: "Tool", Value: instance.ToolName},
		{Key: "Status", Value: instance.Status},
		{Key: "VNC URL", Value: vncURL},
		{Key: "CDP URL", Value: cdpURL},
	}

	if err := f.PrintKeyValue(result); err != nil {
		return err
	}

	if browserTime {
		f.PrintTiming(timing)
	}

	return nil
}

// acquireInstanceToken acquires an access token for the given instance
func acquireInstanceToken(ctx context.Context, instanceID string) (string, error) {
	cloudCfg := config.GetCloudConfig()

	credential := common.NewCredential(cloudCfg.SecretID, cloudCfg.SecretKey)
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = cloudCfg.ControlPlaneEndpoint()

	agsClient, err := ags.NewClient(credential, cloudCfg.Region, cpf)
	if err != nil {
		return "", fmt.Errorf("failed to create AGS client: %w", err)
	}

	tokenResp, err := agsClient.AcquireSandboxInstanceTokenWithContext(ctx, &ags.AcquireSandboxInstanceTokenRequest{
		InstanceId: &instanceID,
	})
	if err != nil {
		return "", err
	}

	if tokenResp.Response == nil || tokenResp.Response.Token == nil {
		return "", fmt.Errorf("no token returned from API")
	}

	return *tokenResp.Response.Token, nil
}

// buildVNCURL constructs the noVNC URL for browser sandbox
func buildVNCURL(instanceID, region, domain, accessToken string, port int) string {
	// Format: https://{port}-{sandbox_id}.{region}.{domain}/novnc/vnc_lite.html?&path=websockify?access_token={token}
	host := fmt.Sprintf("%d-%s.%s.%s", port, instanceID, region, domain)
	return fmt.Sprintf("https://%s/novnc/vnc_lite.html?&path=websockify?access_token=%s", host, accessToken)
}

// buildCDPURL constructs the CDP (Chrome DevTools Protocol) URL for browser sandbox
func buildCDPURL(instanceID, region, domain, accessToken string, port int) string {
	// Format: https://{port}-{sandbox_id}.{region}.{domain}/cdp?access_token={token}
	host := fmt.Sprintf("%d-%s.%s.%s", port, instanceID, region, domain)
	return fmt.Sprintf("https://%s/cdp?access_token=%s", host, accessToken)
}

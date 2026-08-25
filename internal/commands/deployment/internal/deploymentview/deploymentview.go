// Package deploymentview renders Deployment SDK objects consistently across
// deployment resource commands.
package deploymentview

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

// RenderDetails writes a Kubernetes describe-style Deployment view.
func RenderDetails(w io.Writer, deployment *ags.Deployment) {
	if deployment == nil {
		fmt.Fprintln(w, "Deployment: <none>")
		return
	}
	tags := strings.Join(sortedTags(deployment.Tags), ", ")
	if tags == "" {
		tags = "<none>"
	}
	writeField := func(key, value string) {
		if value == "" {
			value = "<none>"
		}
		fmt.Fprintf(w, "%-15s%s\n", key+":", value)
	}
	writeField("Name", derefString(deployment.DeploymentName))
	writeField("ID", derefString(deployment.DeploymentId))
	writeField("Tool", derefString(deployment.ToolId))
	writeField("Status", derefString(deployment.Status))
	if reason := derefString(deployment.StatusReason); reason != "" {
		writeField("Status Reason", reason)
	}
	writeField("Tags", tags)
	writeField("Created", derefString(deployment.CreatedTime))
	writeField("Updated", derefString(deployment.UpdatedTime))

	fmt.Fprintln(w, "Scaling:")
	writeSectionField(w, "Min Instances", int64Value(scalingMin(deployment)))
	writeSectionField(w, "Max Instances", int64Value(scalingMax(deployment)))
	writeSectionField(w, "Max Requests per Instance", int64Value(scalingConcurrency(deployment)))

	fmt.Fprintln(w, "Lifecycle:")
	writeSectionField(w, "Idle Action", lifecycleAction(deployment))
	writeSectionField(w, "Idle Timeout", secondsValue(lifecycleTimeout(deployment)))

	fmt.Fprintln(w, "Affinity:")
	writeSectionField(w, "Mode", affinityMode(deployment))
	writeSectionField(w, "Header", affinityHeader(deployment))
}

// RenderList writes the frozen Deployment list columns and pagination hint.
func RenderList(w io.Writer, deployments []*ags.Deployment, total int, now time.Time) {
	if len(deployments) == 0 {
		fmt.Fprintln(w, "No deployments found")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tTOOL\tSTATUS\tSCALING\tLIFECYCLE\tAFFINITY\tAGE")
	for _, deployment := range deployments {
		if deployment == nil {
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			valueOrDash(derefString(deployment.DeploymentId)),
			valueOrDash(derefString(deployment.DeploymentName)),
			valueOrDash(derefString(deployment.ToolId)),
			valueOrDash(derefString(deployment.Status)),
			ScalingSummary(deployment.ScalingConfiguration),
			LifecycleSummary(deployment.LifecycleConfiguration),
			AffinitySummary(deployment.AffinityConfiguration),
			Age(derefString(deployment.CreatedTime), now),
		)
	}
	_ = tw.Flush()
	if total > len(deployments) {
		fmt.Fprintf(w, "\nShowing %d of %d items (use --offset and --limit for pagination)\n", len(deployments), total)
	}
}

// ScalingSummary returns the compact min/max×per-instance concurrency form.
func ScalingSummary(configuration *ags.ScalingConfiguration) string {
	if configuration == nil {
		return "-"
	}
	return fmt.Sprintf("%s/%s×%s",
		int64Value(configuration.MinInstanceCount),
		int64Value(configuration.MaxInstanceCount),
		int64Value(configuration.MaxInstanceRequestConcurrency),
	)
}

// LifecycleSummary returns the compact ACTION/timeout form.
func LifecycleSummary(configuration *ags.LifecycleConfiguration) string {
	if configuration == nil {
		return "-"
	}
	return fmt.Sprintf("%s/%s", valueOrDash(derefString(configuration.IdleAction)), secondsValue(configuration.IdleTimeoutSeconds))
}

// AffinitySummary returns the configured affinity mode.
func AffinitySummary(configuration *ags.AffinityConfiguration) string {
	if configuration == nil {
		return "-"
	}
	return valueOrDash(derefString(configuration.Mode))
}

// Age returns elapsed time since creation using Kubernetes HumanDuration
// thresholds. Invalid or absent timestamps render as a dash.
func Age(created string, now time.Time) string {
	if created == "" {
		return "-"
	}
	parsed, err := time.Parse(time.RFC3339, created)
	if err != nil {
		return "-"
	}
	return humanDuration(now.Sub(parsed))
}

func humanDuration(duration time.Duration) string {
	if seconds := int(duration.Seconds()); seconds < -1 {
		return "<invalid>"
	} else if seconds < 0 {
		return "0s"
	} else if seconds < 120 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := int(duration / time.Minute)
	if minutes < 10 {
		seconds := int(duration/time.Second) % 60
		if seconds == 0 {
			return fmt.Sprintf("%dm", minutes)
		}
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}
	if minutes < 180 {
		return fmt.Sprintf("%dm", minutes)
	}
	hours := int(duration / time.Hour)
	if hours < 8 {
		minutes = int(duration/time.Minute) % 60
		if minutes == 0 {
			return fmt.Sprintf("%dh", hours)
		}
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	if hours < 48 {
		return fmt.Sprintf("%dh", hours)
	}
	if hours < 24*8 {
		if remainder := hours % 24; remainder != 0 {
			return fmt.Sprintf("%dd%dh", hours/24, remainder)
		}
		return fmt.Sprintf("%dd", hours/24)
	}
	if hours < 24*365*2 {
		return fmt.Sprintf("%dd", hours/24)
	}
	if hours < 24*365*8 {
		days := (hours / 24) % 365
		if days != 0 {
			return fmt.Sprintf("%dy%dd", hours/24/365, days)
		}
	}
	return fmt.Sprintf("%dy", hours/24/365)
}

func writeSectionField(w io.Writer, key, value string) {
	if value == "" {
		value = "<none>"
	}
	fmt.Fprintf(w, "  %-30s%s\n", key+":", value)
}

func sortedTags(tags []*ags.Tag) []string {
	values := make(map[string]string, len(tags))
	for _, tag := range tags {
		if tag == nil || tag.Key == nil || tag.Value == nil {
			continue
		}
		values[*tag.Key] = *tag.Value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

func scalingMin(deployment *ags.Deployment) *int64 {
	if deployment.ScalingConfiguration == nil {
		return nil
	}
	return deployment.ScalingConfiguration.MinInstanceCount
}

func scalingMax(deployment *ags.Deployment) *int64 {
	if deployment.ScalingConfiguration == nil {
		return nil
	}
	return deployment.ScalingConfiguration.MaxInstanceCount
}

func scalingConcurrency(deployment *ags.Deployment) *int64 {
	if deployment.ScalingConfiguration == nil {
		return nil
	}
	return deployment.ScalingConfiguration.MaxInstanceRequestConcurrency
}

func lifecycleAction(deployment *ags.Deployment) string {
	if deployment.LifecycleConfiguration == nil {
		return ""
	}
	return derefString(deployment.LifecycleConfiguration.IdleAction)
}

func lifecycleTimeout(deployment *ags.Deployment) *int64 {
	if deployment.LifecycleConfiguration == nil {
		return nil
	}
	return deployment.LifecycleConfiguration.IdleTimeoutSeconds
}

func affinityMode(deployment *ags.Deployment) string {
	if deployment.AffinityConfiguration == nil {
		return ""
	}
	return derefString(deployment.AffinityConfiguration.Mode)
}

func affinityHeader(deployment *ags.Deployment) string {
	if deployment.AffinityConfiguration == nil {
		return ""
	}
	return derefString(deployment.AffinityConfiguration.HeaderName)
}

func secondsValue(seconds *int64) string {
	if seconds == nil {
		return "-"
	}
	return compactDuration(time.Duration(*seconds) * time.Second)
}

func compactDuration(duration time.Duration) string {
	if duration >= time.Hour && duration%time.Hour == 0 {
		return fmt.Sprintf("%dh", int64(duration/time.Hour))
	}
	if duration >= time.Minute && duration%time.Minute == 0 {
		return fmt.Sprintf("%dm", int64(duration/time.Minute))
	}
	return fmt.Sprintf("%ds", int64(duration/time.Second))
}

func int64Value(value *int64) string {
	if value == nil {
		return "-"
	}
	return fmt.Sprintf("%d", *value)
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

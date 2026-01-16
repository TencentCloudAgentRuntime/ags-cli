package client

import (
	"fmt"
	"strings"
)

// ============================================================================
// Storage Types
// ============================================================================

// StorageType represents the type of storage
type StorageType string

const (
	StorageTypeCos StorageType = "cos"
	// StorageTypeCfs StorageType = "cfs" // Reserved for future CFS support
)

// StorageMount represents storage mount configuration at tool level
type StorageMount struct {
	Name          string         `json:"name"`           // Mount name, DNS-1123 format, max 63 chars
	StorageSource *StorageSource `json:"storage_source"` // Storage source configuration
	MountPath     string         `json:"mount_path"`     // Default mount path in container
	ReadOnly      bool           `json:"read_only"`      // Default read-only permission
}

// StorageSource represents storage source configuration (COS or future CFS)
type StorageSource struct {
	Cos *CosStorageSource `json:"cos,omitempty"` // COS object storage
	// Cfs *CfsStorageSource `json:"cfs,omitempty"` // Reserved for future CFS support
}

// CosStorageSource represents COS storage source configuration
type CosStorageSource struct {
	Endpoint   string `json:"endpoint,omitempty"` // COS endpoint (optional, defaults to current region)
	BucketName string `json:"bucket_name"`        // COS bucket name
	BucketPath string `json:"bucket_path"`        // Path in bucket, must start with /
}

// GetType returns the storage source type
func (s *StorageSource) GetType() StorageType {
	if s.Cos != nil {
		return StorageTypeCos
	}
	return ""
}

// Validate validates the storage source configuration
func (s *StorageSource) Validate() error {
	if s.Cos == nil {
		return fmt.Errorf("storage source must specify cos configuration")
	}
	return nil
}

// MountOption represents mount option at instance level (override tool defaults)
type MountOption struct {
	Name      string `json:"name"`                 // Match StorageMount name in tool
	MountPath string `json:"mount_path,omitempty"` // Override mount path (optional)
	SubPath   string `json:"sub_path,omitempty"`   // Sub-directory isolation (optional)
	ReadOnly  *bool  `json:"read_only,omitempty"`  // Override read-only (can only tighten, not loosen)
}

// FormatStorageMountSummary returns a brief summary of storage mounts
func FormatStorageMountSummary(mounts []StorageMount) string {
	if len(mounts) == 0 {
		return "-"
	}
	var parts []string
	for _, m := range mounts {
		storageType := "unknown"
		if m.StorageSource != nil {
			storageType = string(m.StorageSource.GetType())
		}
		parts = append(parts, fmt.Sprintf("%s(%s)", m.Name, storageType))
	}
	return strings.Join(parts, ", ")
}

// ============================================================================
// Network Types
// ============================================================================

// VPCConfig represents VPC network configuration
type VPCConfig struct {
	SubnetIds        []string `json:"subnet_ids"`         // VPC subnet ID list
	SecurityGroupIds []string `json:"security_group_ids"` // Security group ID list
}

// FormatVPCConfigSummary formats VPC config for display
func FormatVPCConfigSummary(vpc *VPCConfig) (subnets, secGroups string) {
	if vpc == nil {
		return "-", "-"
	}
	if len(vpc.SubnetIds) > 0 {
		subnets = strings.Join(vpc.SubnetIds, ", ")
	} else {
		subnets = "-"
	}
	if len(vpc.SecurityGroupIds) > 0 {
		secGroups = strings.Join(vpc.SecurityGroupIds, ", ")
	} else {
		secGroups = "-"
	}
	return
}

// ============================================================================
// Tool Types
// ============================================================================

// Tool represents a sandbox tool type
type Tool struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	Type          string            `json:"type"`                   // code-interpreter, browser
	NetworkMode   string            `json:"network_mode,omitempty"` // PUBLIC, VPC, SANDBOX, INTERNAL_SERVICE
	VPCConfig     *VPCConfig        `json:"vpc_config,omitempty"`   // VPC configuration (only when NetworkMode=VPC)
	Tags          map[string]string `json:"tags,omitempty"`
	RoleArn       string            `json:"role_arn,omitempty"`       // Role ARN for storage access
	StorageMounts []StorageMount    `json:"storage_mounts,omitempty"` // Storage mount configurations
	CreatedAt     string            `json:"created_at,omitempty"`     // Creation time (ISO8601)
}

// CreateToolOptions represents options for creating a tool
type CreateToolOptions struct {
	Name           string            // Tool name (required)
	Type           string            // Tool type: code-interpreter or browser (required)
	Description    string            // Tool description (optional)
	DefaultTimeout string            // Default timeout, e.g., "5m", "300s", "1h" (optional)
	NetworkMode    string            // Network mode: PUBLIC, VPC, SANDBOX, INTERNAL_SERVICE (optional, default PUBLIC)
	VPCConfig      *VPCConfig        // VPC configuration (required when NetworkMode=VPC)
	Tags           map[string]string // Tags (optional)
	RoleArn        string            // Role ARN for COS access (required when StorageMounts is set)
	StorageMounts  []StorageMount    // Storage mount configurations (optional)
}

// UpdateToolOptions represents options for updating a tool
type UpdateToolOptions struct {
	ToolID      string            // Tool ID (required)
	Description *string           // Tool description (optional, nil means no change)
	NetworkMode *string           // Network mode: PUBLIC, SANDBOX, INTERNAL_SERVICE (optional, nil means no change; VPC cannot be changed)
	Tags        map[string]string // Tags (optional, nil means no change, empty map clears all tags)
}

// ListToolsOptions represents options for listing tools
type ListToolsOptions struct {
	ToolIDs          []string          // Specific tool IDs to query (max 100)
	Offset           int               // Pagination offset (ignored when ToolIDs specified)
	Limit            int               // Pagination limit, default 20, max 100 (ignored when ToolIDs specified)
	Status           string            // Filter by status: CREATING, ACTIVE, DELETING, FAILED
	ToolType         string            // Filter by type: browser, code-interpreter, computer, mobile
	CreatedSince     string            // Relative time filter, e.g., "5s", "2m", "3h"
	CreatedSinceTime string            // Absolute time filter (RFC3339), e.g., "2024-01-15T10:30:00Z"
	Tags             map[string]string // Filter by tags (tag:<key>=<value>)
}

// ListToolsResult represents the result of listing tools
type ListToolsResult struct {
	Tools      []Tool // List of tools
	TotalCount int    // Total count of tools matching the filter
}

// ============================================================================
// Instance Types
// ============================================================================

// Endpoint represents an access endpoint for sandbox instance
type Endpoint struct {
	Scheme string `json:"scheme"` // http, https, ws, wss
	Scope  string `json:"scope"`  // internet, intranet
	URL    string `json:"url"`    // Full endpoint URL
}

// Instance represents a sandbox instance
type Instance struct {
	ID             string        `json:"id"`
	ToolID         string        `json:"tool_id"`
	ToolName       string        `json:"tool_name"`
	Status         string        `json:"status"`
	CreatedAt      string        `json:"created_at"`
	UpdatedAt      string        `json:"updated_at,omitempty"`
	TimeoutSeconds *uint64       `json:"timeout_seconds,omitempty"` // nil means using tool/system default
	ExpiresAt      string        `json:"expires_at,omitempty"`
	StopReason     string        `json:"stop_reason,omitempty"` // manual, timeout, lifetime_expired, error, system
	Endpoints      []Endpoint    `json:"endpoints,omitempty"`
	AccessToken    string        `json:"access_token,omitempty"`
	Domain         string        `json:"domain,omitempty"`
	MountOptions   []MountOption `json:"mount_options,omitempty"` // Mount options used by this instance
}

// CreateInstanceOptions represents options for creating an instance
type CreateInstanceOptions struct {
	ToolID       string
	ToolName     string        // e.g., "code-interpreter-v1"
	Timeout      int           // timeout in seconds
	MountOptions []MountOption // Mount options to override tool defaults (optional)
}

// ListInstancesOptions represents options for listing instances
type ListInstancesOptions struct {
	InstanceIDs      []string // Specific instance IDs to query (max 100)
	ToolID           string   // Filter by tool ID
	Offset           int      // Pagination offset (ignored when InstanceIDs specified)
	Limit            int      // Pagination limit, default 20, max 100 (ignored when InstanceIDs specified)
	Status           string   // Filter by status: STARTING, RUNNING, FAILED, STOPPING, STOPPED, STARTING_FAILED, STOPPING_FAILED
	CreatedSince     string   // Relative time filter, e.g., "5s", "2m", "3h"
	CreatedSinceTime string   // Absolute time filter (RFC3339), e.g., "2024-01-15T10:30:00Z"
}

// ListInstancesResult represents the result of listing instances
type ListInstancesResult struct {
	Instances  []Instance // List of instances
	TotalCount int        // Total count of instances matching the filter
}

// ============================================================================
// Execution Types
// ============================================================================

// ExecuteResult represents the result of code execution
type ExecuteResult struct {
	Stdout   []string         `json:"stdout,omitempty"`
	Stderr   []string         `json:"stderr,omitempty"`
	Results  []map[string]any `json:"results,omitempty"`
	Error    *ExecutionError  `json:"error,omitempty"`
	ExitCode int              `json:"exit_code"`
}

// ExecutionError represents an execution error
type ExecutionError struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Traceback string `json:"traceback"`
}

// StreamCallbacks holds callbacks for streaming output
type StreamCallbacks struct {
	OnStdout func(string)
	OnStderr func(string)
}

// ============================================================================
// Multi-Task Execution Types
// ============================================================================

// ExecutionTask represents a single execution task
type ExecutionTask struct {
	ID         int    // Task sequence number (starting from 1)
	Code       string // Code to execute
	Source     string // Source: filename or "<code>" for -c input
	InstanceNo int    // Instance number (when repeat > 1)
}

// TaskResult represents the result of a single task execution
type TaskResult struct {
	ID         int            `json:"id"`                    // Task sequence number
	Source     string         `json:"source"`                // Source filename or "<code>"
	InstanceNo int            `json:"instance,omitempty"`    // Instance number (when repeat > 1)
	Result     *ExecuteResult `json:"-"`                     // Execution result (embedded in JSON)
	Error      error          `json:"-"`                     // Execution error (not in JSON)
	ErrorMsg   string         `json:"error_msg,omitempty"`   // Error message for JSON
	DurationMs int64          `json:"duration_ms,omitempty"` // Duration in milliseconds
}

// MultiTaskResult represents the result of multiple task executions
type MultiTaskResult struct {
	Tasks   []TaskResultJSON `json:"tasks"`
	Summary TaskSummary      `json:"summary"`
}

// TaskResultJSON is the JSON representation of a task result
type TaskResultJSON struct {
	ID         int              `json:"id"`
	Source     string           `json:"source"`
	InstanceNo int              `json:"instance,omitempty"`
	Stdout     []string         `json:"stdout"`
	Stderr     []string         `json:"stderr"`
	Results    []map[string]any `json:"results"`
	Error      *ExecutionError  `json:"error"`
	ErrorMsg   string           `json:"error_msg,omitempty"`
	DurationMs int64            `json:"duration_ms"`
}

// TaskSummary represents the summary of multi-task execution
type TaskSummary struct {
	Total      int   `json:"total"`
	Success    int   `json:"success"`
	Failed     int   `json:"failed"`
	DurationMs int64 `json:"duration_ms"`
}

// ============================================================================
// API Key Types
// ============================================================================

// APIKey represents an API key
type APIKey struct {
	KeyID     string `json:"key_id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	MaskedKey string `json:"masked_key"`
	CreatedAt string `json:"created_at"`
}

// CreateAPIKeyResult represents the result of creating an API key
type CreateAPIKeyResult struct {
	KeyID  string `json:"key_id"`
	Name   string `json:"name"`
	APIKey string `json:"api_key"` // Only returned once at creation
}

// ============================================================================
// Utility Functions
// ============================================================================

// derefString safely dereferences a string pointer
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// derefInt safely dereferences an int64 pointer and converts to int
func derefInt(i *int64) int {
	if i == nil {
		return 0
	}
	return int(*i)
}

// getString safely gets a string value from a map
func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

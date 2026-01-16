package client

import (
	"fmt"
	"strings"
)

// ParseStorageMount parses --mount parameter string into StorageMount
// Format: "type=cos,name=<name>,bucket=<bucket>,src=<source-path>,dst=<target-path>[,readonly][,endpoint=<endpoint>]"
func ParseStorageMount(s string) (*StorageMount, error) {
	params := parseKeyValuePairs(s)

	storageType := params["type"]
	if storageType == "" {
		return nil, fmt.Errorf("type is required (currently supported: cos)")
	}

	mount := &StorageMount{
		Name:          params["name"],
		MountPath:     params["dst"],
		ReadOnly:      params["readonly"] != "",
		StorageSource: &StorageSource{},
	}

	// Validate common required fields
	if mount.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if mount.MountPath == "" {
		return nil, fmt.Errorf("dst is required")
	}
	if !strings.HasPrefix(mount.MountPath, "/") {
		return nil, fmt.Errorf("dst must be absolute path (start with /)")
	}

	// Parse type-specific configuration
	switch StorageType(storageType) {
	case StorageTypeCos:
		cos, err := parseCosSource(params)
		if err != nil {
			return nil, fmt.Errorf("cos config error: %w", err)
		}
		mount.StorageSource.Cos = cos

	default:
		return nil, fmt.Errorf("unsupported storage type: %s (currently supported: cos)", storageType)
	}

	return mount, nil
}

// parseCosSource parses COS configuration from parameters
func parseCosSource(params map[string]string) (*CosStorageSource, error) {
	cos := &CosStorageSource{
		BucketName: params["bucket"],
		BucketPath: params["src"],
		Endpoint:   params["endpoint"],
	}

	if cos.BucketName == "" {
		return nil, fmt.Errorf("bucket is required")
	}
	if cos.BucketPath == "" {
		return nil, fmt.Errorf("src is required")
	}
	if !strings.HasPrefix(cos.BucketPath, "/") {
		return nil, fmt.Errorf("src must be absolute path (start with /)")
	}

	return cos, nil
}

// ParseMountOption parses --mount-option parameter string into MountOption
// Format: "name=<name>[,dst=<target-path>][,subpath=<sub-path>][,readonly]"
func ParseMountOption(s string) (*MountOption, error) {
	params := parseKeyValuePairs(s)

	opt := &MountOption{
		Name:      params["name"],
		MountPath: params["dst"],
		SubPath:   params["subpath"],
	}

	// Handle readonly flag
	if params["readonly"] != "" {
		readOnly := true
		opt.ReadOnly = &readOnly
	}

	if opt.Name == "" {
		return nil, fmt.Errorf("name is required")
	}

	// Validate mount path if provided
	if opt.MountPath != "" && !strings.HasPrefix(opt.MountPath, "/") {
		return nil, fmt.Errorf("dst must be absolute path (start with /)")
	}

	return opt, nil
}

// parseKeyValuePairs parses "key=value,key2=value2" format string
func parseKeyValuePairs(s string) map[string]string {
	result := make(map[string]string)
	parts := strings.Split(s, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		kv := strings.SplitN(part, "=", 2)
		key := strings.TrimSpace(kv[0])

		if len(kv) == 2 {
			result[key] = strings.TrimSpace(kv[1])
		} else {
			// Boolean flag like "readonly"
			result[key] = "true"
		}
	}

	return result
}

// FormatStorageMountHelp returns help text for --mount parameter
func FormatStorageMountHelp() string {
	return `Storage mount configuration in key=value format.

COS storage format:
  type=cos,name=<name>,bucket=<bucket>,src=<source-path>,dst=<target-path>[,readonly][,endpoint=<endpoint>]

Parameters:
  type      Storage type (required): cos
  name      Mount name, DNS-1123 format (required)
  bucket    COS bucket name (required for cos)
  src       Source path in bucket, must start with / (required for cos)
  dst       Target mount path in container, must start with / (required)
  readonly  Mount as read-only (optional flag)
  endpoint  COS endpoint (optional, defaults to current region)

Examples:
  --mount "type=cos,name=data,bucket=my-bucket-1250000000,src=/data,dst=/mnt/data"
  --mount "type=cos,name=models,bucket=model-bucket,src=/models,dst=/mnt/models,readonly"`
}

// FormatMountOptionHelp returns help text for --mount-option parameter
func FormatMountOptionHelp() string {
	return `Mount option to override tool storage configuration.

Format:
  name=<name>[,dst=<target-path>][,subpath=<sub-path>][,readonly]

Parameters:
  name      Storage mount name defined in tool (required)
  dst       Override target mount path in container (optional)
  subpath   Sub-directory isolation path (optional)
  readonly  Force read-only mount (optional, can only tighten permissions)

Examples:
  --mount-option "name=data,dst=/workspace,subpath=user-123"
  --mount-option "name=models,readonly"`
}

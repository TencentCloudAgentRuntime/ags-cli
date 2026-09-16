// Package toolcopy projects a tool onto the active create contract.
package toolcopy

import (
	"fmt"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/tooltags"
)

// Request copies all current create-capable fields, including preview additions.
// Response-only fields are deliberately omitted using the request contract.
func Request(value any) (map[string]any, error) {
	tool, err := apivalue.Decode(value)
	if err != nil {
		return nil, err
	}
	catalog, err := apimeta.Get()
	if err != nil {
		return nil, err
	}
	spec := catalog.Spec
	req := project(spec, spec.Actions["CreateSandboxTool"].Input, tool)
	delete(req, "ToolName")
	delete(req, "ClientToken")
	if tool["DefaultTimeoutSeconds"] != nil {
		req["DefaultTimeout"] = fmt.Sprintf("%ds", tool.Int64("DefaultTimeoutSeconds"))
	}
	delete(req, "Tags")
	if tags := tooltags.FilterInheritedValue(tool["Tags"]); len(tags) > 0 {
		req["Tags"] = tags
	}
	if custom, ok := req["CustomConfiguration"].(map[string]any); ok {
		mode, _ := custom["ImageRegistryType"].(string)
		switch strings.ToUpper(mode) {
		case "TCR":
			custom["ImageRegistryType"] = "enterprise"
		case "CCR":
			custom["ImageRegistryType"] = "personal"
		}
	}
	return req, nil
}

func project(spec *apimeta.Spec, name string, source apivalue.Object) map[string]any {
	result := map[string]any{}
	object := spec.Object(name)
	if object == nil {
		return result
	}
	for _, member := range object.Members {
		value := source[member.Name]
		if value == nil || member.Disabled {
			continue
		}
		if member.Type == "object" {
			obj, err := apivalue.Decode(value)
			if err == nil {
				value = project(spec, member.Member, obj)
			}
		} else if (member.Type == "list" || member.Type == "array") && spec.Object(member.Member) != nil {
			if items := source.Objects(member.Name); items != nil {
				projected := make([]map[string]any, 0, len(items))
				for _, item := range items {
					projected = append(projected, project(spec, member.Member, item))
				}
				value = projected
			}
		}
		result[member.Name] = value
	}
	return result
}

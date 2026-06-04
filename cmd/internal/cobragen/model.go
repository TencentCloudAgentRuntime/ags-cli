package main

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
)

func inputsFor(m apimeta.Member, fm *apimeta.FieldMapping) []apimeta.InputMapping {
	if fm != nil && len(fm.Inputs) > 0 {
		out := append([]apimeta.InputMapping(nil), fm.Inputs...)
		for i := range out {
			if out[i].Type == "" {
				out[i].Type = apimeta.ScalarFlagType(m)
			}
		}
		return out
	}
	in := apimeta.InputMapping{Flag: apimeta.KebabCase(m.Name), Type: apimeta.ScalarFlagType(m)}
	if fm != nil {
		if fm.Flag != "" {
			in.Flag = fm.Flag
		}
		in.Shorthand = fm.Shorthand
	}
	return []apimeta.InputMapping{in}
}

func inputHelp(h *apimeta.Help, commandID, fieldName, flagName, fallback string) apimeta.InputHelp {
	if h != nil {
		if ch, ok := h.Commands[commandID]; ok {
			if fh, ok := ch.Fields[fieldName]; ok {
				if ih, ok := fh.Inputs[flagName]; ok {
					if ih.Usage == "" {
						ih.Usage = fh.Description
					}
					if ih.Usage == "" {
						ih.Usage = fallback
					}
					return ih
				}
				if fh.Description != "" {
					return apimeta.InputHelp{Usage: fh.Description}
				}
			}
		}
	}
	return apimeta.InputHelp{Usage: fallback}
}

func commandShortFromHelp(help *apimeta.Help, commandID string) string {
	if help != nil {
		if h := help.Commands[commandID]; h.Short != "" {
			return h.Short
		}
	}
	if h := apimeta.CommandHelpFor(commandID); h.Short != "" {
		return h.Short
	}
	return "Run " + commandID
}

func commandLongFromHelp(help *apimeta.Help, commandID string) string {
	h := apimeta.CommandHelpFor(commandID)
	if help != nil {
		h = help.Commands[commandID]
	}
	return h.Long
}

func commandExamplesFromHelp(help *apimeta.Help, commandID string) []string {
	h := apimeta.CommandHelpFor(commandID)
	if help != nil {
		h = help.Commands[commandID]
	}
	return append([]string(nil), h.Examples...)
}

func groupShort(group string) string {
	switch group {
	case "instance":
		return "Manage sandbox instances"
	case "tool":
		return "Manage sandbox tools"
	case "apikey":
		return "Manage API keys"
	case "image":
		return "Manage images"
	case "pre-cache-image-task":
		return "Manage image pre-cache tasks"
	}
	return "Manage " + group
}

func groupLong(group string) string {
	switch group {
	case "instance":
		return "Manage sandbox instances and related data-plane workflows."
	case "tool":
		return "Manage sandbox tools (templates). Tools define the type and capabilities of sandbox instances."
	case "apikey":
		return "Manage API keys for AGR sandbox access."
	}
	return ""
}

func parserForField(m apimeta.Member, fm *apimeta.FieldMapping) string {
	if fm != nil && fm.Parser != "" {
		return fm.Parser
	}
	inputType := apimeta.ScalarFlagType(m)
	if fm != nil && len(fm.Inputs) > 0 {
		inputType = fm.Inputs[0].Type
		if inputType == "" {
			inputType = apimeta.ScalarFlagType(m)
		}
	}
	switch inputType {
	case "bool":
		return "common.default_bool"
	case "int", "int64", "integer", "uint", "uint64":
		return "common.default_int"
	case "string_array":
		return "common.default_string_array"
	case "json":
		return "common.default_json"
	default:
		return "common.default_string"
	}
}

func subgroupShort(group, sub string) string {
	if group == "image" && sub == "precache" {
		return "Manage image pre-cache tasks"
	}
	return "Manage " + sub
}

func aliasesForGroup(group string) []string {
	switch group {
	case "instance":
		return []string{"i"}
	case "tool":
		return []string{"t"}
	case "apikey":
		return []string{"ak", "key"}
	case "pre-cache-image-task":
		return []string{"pre_cache_image_task"}
	}
	return nil
}

func aliasesForCommand(commandID string) []string {
	switch commandID {
	case "instance.create":
		return []string{"c"}
	case "instance.list", "tool.list", "apikey.list":
		return []string{"ls"}
	case "instance.delete", "tool.delete", "apikey.delete":
		return []string{"rm", "del"}
	}
	return nil
}

func argsForCommand(commandID string) string {
	switch commandID {
	case "instance.update", "instance.pause", "instance.resume", "tool.update":
		return "cli.ArgsExactUnlessSkeleton(1)"
	case "apikey.delete", "pre-cache-image-task.get":
		return "cli.ArgsExactUnlessSkeleton(1)"
	case "instance.delete", "tool.delete":
		return "func(cmd *cobra.Command, args []string) error { if cli.GenerateSkeletonEnabled() { return nil }; return cobra.MinimumNArgs(1)(cmd, args) }"
	default:
		return "cobra.NoArgs"
	}
}

func useForCommand(commandID string) string {
	switch commandID {
	case "instance.update":
		return "update <instance-id>"
	case "instance.pause":
		return "pause <instance-id>"
	case "instance.resume":
		return "resume <instance-id>"
	case "instance.delete":
		return "delete <instance-id> [instance-id...]"
	case "tool.update":
		return "update <tool-id>"
	case "tool.delete":
		return "delete <tool-id> [tool-id...]"
	case "apikey.delete":
		return "delete <key-id>"
	case "pre-cache-image-task.create":
		return "create --image <image> --image-registry-type <type>"
	case "pre-cache-image-task.get":
		return "get <image-digest> --image <image> --image-registry-type <type>"
	}
	parts := strings.Split(commandID, ".")
	return parts[len(parts)-1]
}

func hookForCommand(commandID string) string {
	switch commandID {
	case "instance.create":
		return "instanceCreateFn"
	case "instance.list":
		return "instanceListFn"
	case "instance.update":
		return "instanceUpdateFn"
	case "instance.pause":
		return "instancePauseFn"
	case "instance.resume":
		return "instanceResumeFn"
	case "instance.delete":
		return "instanceDeleteFn"
	case "tool.create":
		return "toolCreateFn"
	case "tool.list":
		return "toolListFn"
	case "tool.update":
		return "toolUpdateFn"
	case "tool.delete":
		return "toolDeleteFn"
	case "apikey.create":
		return "apikeyCreateFn"
	case "apikey.list":
		return "apikeyListFn"
	case "apikey.delete":
		return "apikeyDeleteFn"
	case "pre-cache-image-task.create":
		return "imagePrecacheCreateFn"
	case "pre-cache-image-task.get":
		return "imagePrecacheGetFn"
	}
	return "cli.UnsupportedGeneratedCommand(" + quote(commandID) + ")"
}

func goTypeForInput(t string) string {
	switch t {
	case "string_array":
		return "[]string"
	case "bool":
		return "bool"
	case "int", "int64", "integer":
		return "int"
	default:
		return "string"
	}
}

func cobraTypeForInput(t string) string {
	switch t {
	case "string_array":
		return "stringArray"
	case "bool":
		return "bool"
	case "int", "int64", "integer":
		return "int"
	default:
		return "string"
	}
}

func packageForCommand(parts []string) string {
	return strings.ReplaceAll(parts[len(parts)-1], "-", "")
}

func dirForCommand(parts []string) string {
	cleaned := make([]string, len(parts)-1)
	for i, p := range parts[:len(parts)-1] {
		cleaned[i] = strings.ReplaceAll(p, "-", "")
	}
	return strings.Join(cleaned, "/")
}

func exported(s string) string {
	parts := regexp.MustCompile(`[^A-Za-z0-9]+`).Split(s, -1)
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		if len(p) > 1 {
			b.WriteString(p[1:])
		}
	}
	return b.String()
}

func boolDefault(s string) string {
	if s == "true" {
		return "true"
	}
	return "false"
}

func intDefault(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

func strip(s string) string {
	r := strings.NewReplacer("<p>", "", "</p>", "", "<ul>", "", "</ul>", "", "<li>", "", "</li>", "")
	return r.Replace(s)
}

func quote(s string) string { return strconv.Quote(s) }

//go:build preview

package harnesscli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Item is one decoded list element.
type Item = apivalue.Object

// FilterFlag maps a shortcut flag onto one server-side Filter name.
type FilterFlag struct {
	Flag   string
	Filter string
	Usage  string
	Values []string
}

// FilterFlags returns the shortcut flags plus the shared tag and --all flags.
func FilterFlags(filters []FilterFlag) []command.FlagSpec {
	flags := make([]command.FlagSpec, 0, len(filters)+2)
	for _, filter := range filters {
		usage := filter.Usage + fmt.Sprintf(" (filter %s)", filter.Filter)
		if len(filter.Values) > 0 {
			flags = append(flags, EnumFlag(filter.Flag, usage, filter.Values...))
		} else {
			flags = append(flags, StringFlag(filter.Flag, usage))
		}
	}
	return append(flags,
		StringArrayFlag("tag", "Exact tag match as key=value (filter tag:<key>); repeat for more"),
		BoolFlag("all", "Fetch every page instead of one page"),
	)
}

// ListOptions configures List for one resource list Action.
type ListOptions struct {
	Filters   []FilterFlag
	SetField  string
	Exclusive [2]string
	Render    func(io.Writer, []Item, int)
}

const allPageLimit = 100

func listShortcuts(filters []FilterFlag) []string {
	names := []string{"tag", "all"}
	for _, filter := range filters {
		names = append(names, filter.Flag)
	}
	return names
}

// List assembles shortcut and raw filters, rejects duplicate filter names
// locally, and optionally reads every page.
func List(ctx context.Context, req command.Request, env Env, opts ListOptions) (*command.Result, error) {
	shortcuts := listShortcuts(opts.Filters)
	if err := RejectWithRequest(req, shortcuts...); err != nil {
		return nil, err
	}
	if a, b := opts.Exclusive[0], opts.Exclusive[1]; a != "" && Changed(req, a) && Changed(req, b) {
		return nil, output.NewUsageError("FLAG_CONFLICT", fmt.Sprintf("--%s and --%s cannot be combined", a, b), "Use an exact name or a substring, not both.")
	}
	all := Bool(req, "all")
	if all {
		if name := ChangedAny(req, "offset", "limit"); name != "" {
			return nil, output.NewUsageError("INVALID_PAGINATION", fmt.Sprintf("--all cannot be combined with --%s", name), "Use --all for every page, or --offset/--limit for one page.")
		}
	}
	request, err := env.Builder.Build(WithoutFlags(req, shortcuts...))
	if err != nil {
		return nil, err
	}
	if err := mergeFilters(req, request, opts.Filters); err != nil {
		return nil, err
	}
	if !all {
		result, response, err := env.Execute(ctx, request)
		if err != nil {
			return nil, err
		}
		items, err := response.ReadObjects(opts.SetField)
		if err != nil {
			return nil, err
		}
		total := int(response.Int64("TotalCount"))
		result.Text = func(w io.Writer) { opts.Render(w, items, total) }
		return result, nil
	}
	request["Limit"] = allPageLimit
	var collected []Item
	var base *command.Result
	var last apivalue.Object
	for offset := 0; ; {
		request["Offset"] = offset
		result, response, err := env.Execute(ctx, request)
		if err != nil {
			return nil, err
		}
		if base == nil {
			base = result
		}
		last = response
		page, err := response.ReadObjects(opts.SetField)
		if err != nil {
			return nil, err
		}
		collected = append(collected, page...)
		offset += len(page)
		if len(page) < allPageLimit || int64(offset) >= response.Int64("TotalCount") {
			break
		}
	}
	data := map[string]any{opts.SetField: collected, "TotalCount": len(collected), "RequestId": last.String("RequestId")}
	base.Data = data
	base.Text = func(w io.Writer) { opts.Render(w, collected, len(collected)) }
	return base, nil
}

func mergeFilters(req command.Request, request map[string]any, shortcuts []FilterFlag) error {
	var filters []any
	if raw, ok := request["Filters"].([]any); ok {
		filters = raw
	}
	seen := map[string]bool{}
	for _, raw := range filters {
		filter, _ := raw.(map[string]any)
		name, _ := filter["Name"].(string)
		if seen[name] {
			return duplicateFilter(name)
		}
		seen[name] = true
	}
	add := func(name string, values ...string) error {
		if seen[name] {
			return duplicateFilter(name)
		}
		seen[name] = true
		filters = append(filters, map[string]any{"Name": name, "Values": values})
		return nil
	}
	for _, shortcut := range shortcuts {
		if !Changed(req, shortcut.Flag) {
			continue
		}
		value := strings.TrimSpace(String(req, shortcut.Flag))
		if value == "" {
			return output.NewUsageError("INVALID_FILTER", fmt.Sprintf("--%s cannot be empty", shortcut.Flag), "Omit the flag to skip this filter.")
		}
		if err := add(shortcut.Filter, value); err != nil {
			return err
		}
	}
	tagValues := map[string][]string{}
	for _, value := range Strings(req, "tag") {
		key, tagValue, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return output.NewUsageError("INVALID_TAG", fmt.Sprintf("--tag %q must be key=value", value), "Example: --tag env=prod")
		}
		tagValues[strings.TrimSpace(key)] = append(tagValues[strings.TrimSpace(key)], tagValue)
	}
	for _, key := range slices.Sorted(maps.Keys(tagValues)) {
		if err := add("tag:"+key, tagValues[key]...); err != nil {
			return err
		}
	}
	if len(filters) > 0 {
		request["Filters"] = filters
	}
	return nil
}

func duplicateFilter(name string) error {
	return output.NewUsageError("DUPLICATE_FILTER", fmt.Sprintf("filter %q is set more than once", name),
		"Set each filter once, either with its shortcut flag or in --filters.")
}

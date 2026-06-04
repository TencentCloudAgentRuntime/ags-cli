// Package cmdtree projects command.Registry metadata into Cobra commands and
// wires normalized command requests to module runtimes.
package cmdtree

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Options controls how a command registry is projected into Cobra commands.
type Options struct {
	RootUse   string
	RootShort string
	// AllowMissingGroupMetadata lets transitional callers synthesize group
	// commands when a module path references an undeclared parent.
	AllowMissingGroupMetadata bool
	// ModuleCommandBuilder overrides leaf command construction for tests and
	// migration adapters. Production uses BuildModuleCommand.
	ModuleCommandBuilder func(command.Module, command.Deps) (*cobra.Command, error)
}

// Build creates a root Cobra command and attaches every registered command.
// Callers that already own a root command should use AddTo instead.
func Build(registry *command.Registry, deps command.Deps, opts Options) (*cobra.Command, error) {
	root := &cobra.Command{
		Use:           defaultString(opts.RootUse, "agr"),
		Short:         opts.RootShort,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	if err := AddTo(root, registry, deps, opts); err != nil {
		return nil, err
	}
	return root, nil
}

// AddTo attaches every registered group and module below an existing root
// command. Groups are attached before modules, and sibling names/aliases are
// tracked to catch collisions before Cobra's help output becomes ambiguous.
func AddTo(root *cobra.Command, registry *command.Registry, deps command.Deps, opts Options) error {
	if root == nil {
		return fmt.Errorf("cmdtree root is nil")
	}
	if registry == nil {
		return fmt.Errorf("cmdtree registry is nil")
	}
	deps = deps.WithDefaults()
	nodes := map[string]*cobra.Command{"": root}
	siblings := map[string]map[string]string{}

	for _, group := range registry.Groups() {
		if err := addGroup(root, nodes, siblings, group, opts); err != nil {
			return err
		}
	}
	for _, module := range registry.Modules() {
		if err := addModule(root, nodes, siblings, module, deps, opts); err != nil {
			return err
		}
	}
	return nil
}

func addGroup(root *cobra.Command, nodes map[string]*cobra.Command, siblings map[string]map[string]string, group command.GroupSpec, opts Options) error {
	parent, err := ensureParent(root, nodes, siblings, group.Path[:len(group.Path)-1], nil, opts)
	if err != nil {
		return err
	}
	key := key(group.Path)
	if _, exists := nodes[key]; exists {
		return nil
	}
	cmd := &cobra.Command{
		Use:     defaultString(group.Use, group.Path[len(group.Path)-1]),
		Short:   group.Short,
		Long:    group.Long,
		Aliases: group.Aliases,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	if err := attachChild(parent, cmd, siblings, group.Path); err != nil {
		return err
	}
	nodes[key] = cmd
	return nil
}

func addModule(root *cobra.Command, nodes map[string]*cobra.Command, siblings map[string]map[string]string, module command.Module, deps command.Deps, opts Options) error {
	spec := module.Descriptor.Spec
	parentPath := spec.Path[:len(spec.Path)-1]
	parent, err := ensureParent(root, nodes, siblings, parentPath, module.Descriptor.Groups, opts)
	if err != nil {
		return err
	}
	builder := opts.ModuleCommandBuilder
	if builder == nil {
		builder = BuildModuleCommand
	}
	cmd, err := builder(module, deps)
	if err != nil {
		return err
	}
	if err := attachChild(parent, cmd, siblings, spec.Path); err != nil {
		return err
	}
	nodes[key(spec.Path)] = cmd
	return nil
}

func ensureParent(root *cobra.Command, nodes map[string]*cobra.Command, siblings map[string]map[string]string, path []string, moduleGroups []command.GroupSpec, opts Options) (*cobra.Command, error) {
	if len(path) == 0 {
		return root, nil
	}
	current := root
	for i := range path {
		prefix := append([]string(nil), path[:i+1]...)
		prefixKey := key(prefix)
		if existing, ok := nodes[prefixKey]; ok {
			current = existing
			continue
		}
		group, ok := findGroup(moduleGroups, prefix)
		if !ok {
			if !opts.AllowMissingGroupMetadata {
				return nil, fmt.Errorf("missing group metadata for %q", prefixKey)
			}
			group = command.GroupSpec{
				Path:  prefix,
				Use:   prefix[len(prefix)-1],
				Short: fmt.Sprintf("%s commands", prefixKey),
			}
		}
		cmd := &cobra.Command{
			Use:     defaultString(group.Use, prefix[len(prefix)-1]),
			Short:   group.Short,
			Long:    group.Long,
			Aliases: group.Aliases,
			Args:    cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return cmd.Help()
			},
		}
		if err := attachChild(current, cmd, siblings, prefix); err != nil {
			return nil, err
		}
		nodes[prefixKey] = cmd
		current = cmd
	}
	return current, nil
}

func attachChild(parent *cobra.Command, child *cobra.Command, siblings map[string]map[string]string, childPath []string) error {
	parentKey := ""
	if parent.CommandPath() != "" {
		parentKey = parent.CommandPath()
	}
	if siblings[parentKey] == nil {
		siblings[parentKey] = map[string]string{}
	}
	names := append([]string{child.Name()}, child.Aliases...)
	for _, name := range names {
		if name == "" {
			continue
		}
		if owner, exists := siblings[parentKey][name]; exists {
			return fmt.Errorf("duplicate command alias/name %q under %q: %s and %s", name, parent.CommandPath(), owner, key(childPath))
		}
	}
	for _, name := range names {
		if name != "" {
			siblings[parentKey][name] = key(childPath)
		}
	}
	parent.AddCommand(child)
	return nil
}

func validateDescriptor(desc command.Descriptor) error {
	if err := validateSpec(desc.Spec); err != nil {
		return err
	}
	if err := validateGeneratedAPIMetadata(desc); err != nil {
		return err
	}
	if err := validateMixedOutput(desc); err != nil {
		return err
	}
	if err := validateFlagConflicts(desc.Spec); err != nil {
		return err
	}
	return nil
}

// BuildModuleCommand builds a standalone Cobra leaf for one command module.
// Transitional adapters can attach the returned command under an existing
// hand-built group while still sharing cmdtree's validation, flag wiring,
// request normalization, rendering, and exit-code propagation.
func BuildModuleCommand(module command.Module, deps command.Deps) (*cobra.Command, error) {
	spec := module.Descriptor.Spec
	if err := validateDescriptor(module.Descriptor); err != nil {
		return nil, err
	}
	use := defaultString(spec.Use, spec.Path[len(spec.Path)-1])
	long, examples := commandHelpText(spec)
	cmd := &cobra.Command{
		Use:         use,
		Short:       spec.Short,
		Long:        long,
		Example:     formatExampleBlocks(examples),
		Aliases:     spec.Aliases,
		Hidden:      spec.Hidden,
		Args:        argsValidator(spec),
		Annotations: map[string]string{"agr.command.id": spec.ID, "agr.command.source": module.Descriptor.Source},
		RunE: func(cmd *cobra.Command, args []string) error {
			runtime, err := module.Build(deps)
			if err != nil {
				return err
			}
			if runtime.Handler == nil {
				return fmt.Errorf("command %q runtime missing handler", spec.ID)
			}
			req, err := BuildRequest(cmd, spec, args, deps)
			if err != nil {
				return err
			}
			result, err := runtime.Handler.Run(cmd.Context(), req)
			if err != nil {
				return err
			}
			if result == nil {
				return nil
			}
			if result.StreamDone {
				return exitError(result.ExitCode)
			}
			if runtime.Renderer != nil {
				if err := runtime.Renderer.Render(cmd.Context(), result); err != nil {
					return err
				}
			} else if err := defaultRender(deps, result); err != nil {
				return err
			}
			if result.Failure != nil {
				code := result.ExitCode
				if code == 0 {
					code = output.ExitCodeForKind(result.Failure.Kind)
				}
				return &output.CLIError{Failure: result.Failure, ExitCode: code}
			}
			return exitError(result.ExitCode)
		},
	}
	if err := registerFlags(cmd.Flags(), spec.Flags); err != nil {
		return nil, err
	}
	return cmd, nil
}

func commandHelpText(spec command.Spec) (string, []string) {
	long, extracted := splitLongExamples(spec.Long)
	examples := append([]string(nil), spec.Examples...)
	examples = append(examples, extracted...)
	if len(examples) == 0 {
		examples = []string{defaultExample(spec)}
	}
	return long, examples
}

func splitLongExamples(long string) (string, []string) {
	const marker = "\n\nExamples:\n"
	idx := strings.Index(long, marker)
	if idx < 0 {
		return long, nil
	}
	prefix := strings.TrimRight(long[:idx], "\n")
	block := strings.TrimSpace(long[idx+len(marker):])
	if block == "" {
		return prefix, nil
	}
	var examples []string
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		examples = append(examples, line)
	}
	return prefix, examples
}

func formatExampleBlocks(examples []string) string {
	var blocks []string
	for i, example := range examples {
		example = strings.TrimSpace(example)
		if example == "" {
			continue
		}
		if strings.HasPrefix(example, "Example - ") {
			blocks = append(blocks, example)
			continue
		}
		blocks = append(blocks, fmt.Sprintf("Example - %s:\n%s", exampleTitle(i), indentExample(example)))
	}
	return strings.Join(blocks, "\n\n")
}

func exampleTitle(index int) string {
	switch index {
	case 0:
		return "Basic usage"
	case 1:
		return "Common option"
	case 2:
		return "JSON output"
	default:
		return "Advanced usage"
	}
}

func indentExample(example string) string {
	var lines []string
	for _, line := range strings.Split(example, "\n") {
		if strings.TrimSpace(line) == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, "  "+strings.TrimRight(line, " "))
	}
	return strings.Join(lines, "\n")
}

func defaultExample(spec command.Spec) string {
	parts := append([]string{"agr"}, spec.Path...)
	for _, arg := range spec.Args {
		if arg.Required {
			parts = append(parts, placeholderForArg(arg.Name))
		}
	}
	for _, flag := range spec.Flags {
		if flag.Hidden || !flag.Required {
			continue
		}
		parts = append(parts, "--"+flag.Name, placeholderForFlag(flag))
	}
	return strings.Join(parts, " ")
}

func placeholderForArg(name string) string {
	switch strings.ToLower(strings.ReplaceAll(name, "-", "")) {
	case "instanceid":
		return "ins-xxxx"
	case "toolid", "sourcetoolid":
		return "sdt-xxxx"
	case "keyid":
		return "key-xxxx"
	case "imagedigest":
		return "sha256:..."
	case "action":
		return "DescribeSandboxInstanceList"
	default:
		return "<" + strings.ToLower(name) + ">"
	}
}

func placeholderForFlag(flag command.FlagSpec) string {
	switch flag.Type {
	case command.FlagBool:
		return "true"
	case command.FlagInt:
		return "1"
	case command.FlagStringArray:
		return "<value>"
	default:
		return "<value>"
	}
}

func validateSpec(spec command.Spec) error {
	if spec.ID == "" {
		return fmt.Errorf("command spec missing id")
	}
	if len(spec.Path) == 0 {
		return fmt.Errorf("command %q missing path", spec.ID)
	}
	seenAliases := map[string]bool{}
	for _, alias := range spec.Aliases {
		if alias == "" {
			continue
		}
		if seenAliases[alias] {
			return fmt.Errorf("command %q has duplicate alias %q", spec.ID, alias)
		}
		seenAliases[alias] = true
	}
	return nil
}

func validateGeneratedAPIMetadata(desc command.Descriptor) error {
	if desc.Generated == nil {
		if desc.Source == "mixed-api" && desc.API != nil {
			return fmt.Errorf("mixed API command %q missing generated descriptor snapshot", desc.Spec.ID)
		}
		return nil
	}
	generated := desc.Generated.Spec
	final := desc.Spec
	// Mixed modules are allowed to replace runtime behavior, but should not
	// silently drift away from the generated API identity they claim to wrap.
	if generated.ID != "" && final.ID != generated.ID {
		return fmt.Errorf("mixed API command changed generated command id from %q to %q", generated.ID, final.ID)
	}
	if len(generated.Path) > 0 && key(final.Path) != key(generated.Path) {
		return fmt.Errorf("mixed API command %q changed generated path from %q to %q", final.ID, key(generated.Path), key(final.Path))
	}
	generatedAction := apiAction(desc.Generated.API)
	finalAction := apiAction(desc.API)
	if generatedAction != "" && finalAction != generatedAction {
		return fmt.Errorf("mixed API command %q changed generated API action from %q to %q", final.ID, generatedAction, finalAction)
	}
	return nil
}

func validateMixedOutput(desc command.Descriptor) error {
	if desc.API == nil {
		return nil
	}
	if desc.Source == "apicli" {
		return nil
	}
	if desc.Spec.Output.DataType == "" {
		return fmt.Errorf("mixed API command %q must declare output data type", desc.Spec.ID)
	}
	return nil
}

func validateFlagConflicts(spec command.Spec) error {
	shorthands := map[string]command.FlagSpec{}
	names := map[string]command.FlagSpec{}
	for _, flag := range spec.Flags {
		if flag.Name == "" {
			return fmt.Errorf("command %q has flag with empty name", spec.ID)
		}
		if flag.Shorthand != "" {
			if existing, exists := shorthands[flag.Shorthand]; exists {
				if isWorkflowGeneratedConflict(flag, existing) {
					return fmt.Errorf("command %q workflow flag --%s conflicts with generated/API shorthand -%s for --%s", spec.ID, workflowFlagName(flag, existing), flag.Shorthand, generatedFlagName(flag, existing))
				}
				return fmt.Errorf("command %q has duplicate shorthand -%s for --%s and --%s", spec.ID, flag.Shorthand, existing.Name, flag.Name)
			}
			shorthands[flag.Shorthand] = flag
		}
		for _, name := range append([]string{flag.Name}, flag.Aliases...) {
			if existing, exists := names[name]; exists {
				if isWorkflowGeneratedConflict(flag, existing) {
					return fmt.Errorf("command %q workflow flag --%s conflicts with generated/API flag --%s", spec.ID, flag.Name, existing.Name)
				}
				return fmt.Errorf("command %q has duplicate flag name/alias --%s", spec.ID, name)
			}
			names[name] = flag
		}
	}
	return nil
}

func isWorkflowGeneratedConflict(a, b command.FlagSpec) bool {
	return (a.Workflow && b.Generated) || (b.Workflow && a.Generated)
}

func workflowFlagName(a, b command.FlagSpec) string {
	if a.Workflow {
		return a.Name
	}
	return b.Name
}

func generatedFlagName(a, b command.FlagSpec) string {
	if a.Generated {
		return a.Name
	}
	return b.Name
}

func apiAction(api any) string {
	switch v := api.(type) {
	case apicli.APIDescriptor:
		return v.API.Action
	case *apicli.APIDescriptor:
		if v == nil {
			return ""
		}
		return v.API.Action
	case apicli.APISpec:
		return v.Action
	case *apicli.APISpec:
		if v == nil {
			return ""
		}
		return v.Action
	default:
		return ""
	}
}

func registerFlags(flags *pflag.FlagSet, specs []command.FlagSpec) error {
	for i := range specs {
		spec := specs[i]
		if spec.Type == "" {
			spec.Type = command.FlagString
		}
		usage := flagUsage(spec)
		names := append([]string{spec.Name}, spec.Aliases...)
		switch spec.Type {
		case command.FlagString:
			value := stringDefault(spec.Default)
			for _, name := range names {
				flags.StringP(name, shorthandFor(name, spec), value, usage)
			}
		case command.FlagBool:
			value := boolDefault(spec.Default)
			for _, name := range names {
				flags.BoolP(name, shorthandFor(name, spec), value, usage)
			}
		case command.FlagInt:
			value := intDefault(spec.Default)
			for _, name := range names {
				flags.IntP(name, shorthandFor(name, spec), value, usage)
			}
		case command.FlagStringArray:
			value := stringArrayDefault(spec.Default)
			for _, name := range names {
				flags.StringArrayP(name, shorthandFor(name, spec), value, usage)
			}
		default:
			return fmt.Errorf("flag --%s has unsupported type %q", spec.Name, spec.Type)
		}
		for _, name := range names {
			f := flags.Lookup(name)
			if f == nil {
				continue
			}
			f.Hidden = spec.Hidden
			f.Deprecated = spec.Deprecated
			f.Annotations = spec.Annotations
		}
	}
	return nil
}

func flagUsage(spec command.FlagSpec) string {
	usage := strings.TrimSpace(spec.Usage)
	var sections []string
	if spec.Format != "" {
		sections = append(sections, "Format:\n  "+strings.TrimSpace(spec.Format))
	}
	if len(spec.Values) > 0 {
		sections = append(sections, "Values:\n  "+strings.Join(spec.Values, "\n  "))
	}
	if len(spec.Examples) > 0 {
		sections = append(sections, "Examples:\n  "+strings.Join(spec.Examples, "\n  "))
	}
	if len(sections) == 0 {
		return usage
	}
	if usage == "" {
		return strings.Join(sections, "\n\n")
	}
	return usage + "\n\n" + strings.Join(sections, "\n\n")
}

// BuildRequest converts Cobra args and flags into the normalized command
// request consumed by command handlers. It records canonical flag values under
// their declared names while also tracking changed aliases for conflict checks.
func BuildRequest(cmd *cobra.Command, spec command.Spec, args []string, deps command.Deps) (command.Request, error) {
	req := command.Request{
		CommandID:    spec.ID,
		Path:         append([]string(nil), spec.Path...),
		Args:         append([]string(nil), args...),
		ArgValues:    map[string]string{},
		Flags:        map[string]command.FlagValue{},
		ChangedFlags: map[string]bool{},
		RawArgs:      cmd.Flags().Args(),
		DashPos:      cmd.ArgsLenAtDash(),
		Stdin:        deps.Stdin,
	}
	for i, arg := range spec.Args {
		if i < len(args) {
			req.ArgValues[arg.Name] = args[i]
		}
	}
	for _, flag := range spec.Flags {
		value, err := readFlagValue(cmd.Flags(), flag)
		if err != nil {
			return req, err
		}
		if flag.Required && !value.Changed {
			return req, fmt.Errorf("required flag --%s not set", flag.Name)
		}
		req.Flags[flag.Name] = value
		if value.Changed {
			req.ChangedFlags[flag.Name] = true
		}
		for _, alias := range flag.Aliases {
			if cmd.Flags().Changed(alias) {
				req.ChangedFlags[alias] = true
			}
		}
		if flag.Name == "request" && value.Changed && value.String != "" {
			// Preserve the original request payload text so higher layers can
			// distinguish "request mode" from parsed per-flag request assembly.
			req.RawRequest = []byte(value.String)
		}
	}
	return req, nil
}

func readFlagValue(flags *pflag.FlagSet, spec command.FlagSpec) (command.FlagValue, error) {
	out := command.FlagValue{Name: spec.Name, Type: spec.Type}
	names := append([]string{spec.Name}, spec.Aliases...)
	readName := spec.Name
	for _, name := range names {
		if flags.Changed(name) {
			readName = name
			out.Changed = true
			break
		}
	}
	switch spec.Type {
	case "", command.FlagString:
		v, err := flags.GetString(readName)
		out.String, out.Raw = v, v
		return out, err
	case command.FlagBool:
		v, err := flags.GetBool(readName)
		out.Bool, out.Raw = v, v
		return out, err
	case command.FlagInt:
		v, err := flags.GetInt(readName)
		out.Int, out.Raw = v, v
		return out, err
	case command.FlagStringArray:
		v, err := flags.GetStringArray(readName)
		out.Strings, out.Raw = v, v
		return out, err
	default:
		return out, fmt.Errorf("flag --%s has unsupported type %q", spec.Name, spec.Type)
	}
}

func argsValidator(spec command.Spec) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		required := 0
		repeatable := false
		for _, arg := range spec.Args {
			if arg.Required {
				required++
			}
			if arg.Repeatable {
				repeatable = true
			}
		}
		if len(args) < required {
			return fmt.Errorf("%s requires at least %d arg(s), got %d", spec.ID, required, len(args))
		}
		if !repeatable && len(args) > len(spec.Args) {
			return fmt.Errorf("%s accepts at most %d arg(s), got %d", spec.ID, len(spec.Args), len(args))
		}
		return nil
	}
}

func defaultRender(deps command.Deps, result *command.Result) error {
	if result.Text != nil {
		result.Text(deps.IO.Out)
		return nil
	}
	if result.Data != nil {
		enc := json.NewEncoder(deps.IO.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(result.Data)
	}
	for _, warning := range result.Warnings {
		fmt.Fprintln(deps.IO.ErrOut, warning)
	}
	return nil
}

func exitError(code int) error {
	if code == 0 {
		return nil
	}
	return &output.CLIError{
		Failure:  &output.Failure{Code: "COMMAND_FAILED", Kind: output.KindGenericError, Message: "command failed"},
		ExitCode: code,
	}
}

func findGroup(groups []command.GroupSpec, path []string) (command.GroupSpec, bool) {
	want := key(path)
	for _, group := range groups {
		if key(group.Path) == want {
			return group, true
		}
	}
	return command.GroupSpec{}, false
}

func key(path []string) string {
	return strings.Join(path, ".")
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func shorthandFor(name string, spec command.FlagSpec) string {
	if name == spec.Name {
		return spec.Shorthand
	}
	return ""
}

func stringDefault(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func boolDefault(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

func intDefault(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func stringArrayDefault(v any) []string {
	if values, ok := v.([]string); ok {
		return values
	}
	return nil
}

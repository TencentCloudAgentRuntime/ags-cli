// Package command defines the registry-facing command model used by both
// generated API commands and hand-written workflow commands.
package command

import (
	"context"
	"io"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/iostreams"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Module is the concrete command unit registered by generated and hand-written
// command packages. Descriptor is metadata-only and can be inspected without
// credentials or config; Build receives runtime dependencies only when Cobra
// executes the command.
type Module struct {
	Descriptor Descriptor
	Build      func(Deps) (Runtime, error)
}

// Descriptor is safe to read without config, credentials, SDK clients, or
// dataplane clients. API holds descriptor data owned by packages such as apicli.
type Descriptor struct {
	// Spec describes the final public command surface after any hand-written
	// workflow overrides have been applied.
	Spec Spec
	// Generated is an optional metadata snapshot for mixed modules. It records
	// the generated API command before hand-written workflow code adjusts the
	// final Spec.
	Generated *Descriptor
	// Groups contains metadata for each intermediate command path required to
	// attach Spec.Path into the Cobra tree.
	Groups []GroupSpec
	// API carries generator-owned metadata such as apicli.APIDescriptor. The
	// core command package keeps it opaque to avoid importing generator layers.
	API any
	// Source identifies the module family ("apicli", "workflow", "mixed-api")
	// for diagnostics and schema output.
	Source string
}

// Spec describes the public Cobra surface for one leaf command.
type Spec struct {
	// ID is the stable dot-separated command identifier used in JSON envelopes,
	// schema lookup, tests, and generated API metadata.
	ID string
	// Path is the Cobra command path below the root command. It is also used to
	// detect duplicate command locations while building the tree.
	Path     []string
	Use      string
	Short    string
	Long     string
	Examples []string
	Aliases  []string
	Hidden   bool
	// Args and Flags describe the normalized command inputs before they are
	// materialized as Cobra validators and pflag definitions.
	Args           []ArgSpec
	Flags          []FlagSpec
	Output         OutputSpec
	SupportsJSON   bool
	SupportsNDJSON bool
}

// GroupSpec describes an intermediate command group in the command tree.
type GroupSpec struct {
	Path     []string
	Use      string
	Short    string
	Long     string
	Aliases  []string
	Examples []string
}

// ArgSpec describes one positional argument accepted by a command.
type ArgSpec struct {
	Name        string
	Description string
	Required    bool
	Repeatable  bool
}

// FlagType identifies the supported flag value kinds for generated wiring.
type FlagType string

const (
	// FlagString represents a single string flag.
	FlagString FlagType = "string"
	// FlagBool represents a boolean flag.
	FlagBool FlagType = "bool"
	// FlagInt represents an integer flag.
	FlagInt FlagType = "int"
	// FlagStringArray represents a repeatable string-array flag.
	FlagStringArray FlagType = "stringArray"
)

// FlagSpec describes one CLI flag attached to a command.
type FlagSpec struct {
	Name      string
	Shorthand string
	Aliases   []string
	Usage     string
	Type      FlagType
	Default   any
	Required  bool
	Hidden    bool
	// Workflow marks hand-written workflow flags so cmdtree can produce a
	// clearer error when they collide with generated API flags.
	Workflow bool
	// Generated marks flags derived from API metadata rather than handwritten
	// command code. It is used for conflict diagnostics and schema reporting.
	Generated   bool
	Deprecated  string
	Annotations map[string][]string
	// Format describes the expected shape for complex flag values.
	Format string
	// Examples lists concrete invocations for complex flag values.
	Examples []string
	// Values lists supported values for enum-like flags.
	Values []string
}

// OutputSpec describes the canonical data shape and effects a command returns.
type OutputSpec struct {
	DataType    string
	Description string
	// Effects lists resource mutations such as creating or deleting instances.
	// The JSON envelope exposes these so automation can reason about side effects.
	Effects []string
}

// FlagValue is the runtime value captured from one parsed Cobra flag.
type FlagValue struct {
	Name    string
	Type    FlagType
	Changed bool
	String  string
	Strings []string
	Bool    bool
	Int     int
	Raw     any
}

// Request is the normalized command invocation passed to command handlers.
type Request struct {
	CommandID    string
	Path         []string
	Args         []string
	ArgValues    map[string]string
	Flags        map[string]FlagValue
	ChangedFlags map[string]bool
	// RawRequest preserves the original --request payload after Cobra parsing.
	// Generated API request builders use it to choose raw JSON mode.
	RawRequest []byte
	// RawArgs and DashPos expose Cobra's post-flag argument view for commands
	// such as exec/adb that forward arguments after "--".
	RawArgs []string
	DashPos int
	Stdin   io.Reader
}

// Result is the normalized command outcome consumed by renderers.
type Result struct {
	Data     any
	Text     func(io.Writer)
	Warnings []string
	Effects  []output.Effect
	Failure  *output.Failure
	ExitCode int
	// StreamDone means the handler already streamed output and only the exit
	// code should be propagated by the wrapper.
	StreamDone bool
	// MetaExtra is merged into the JSON envelope's Meta object for
	// command-specific metadata such as API action or request mode.
	MetaExtra map[string]any
}

// Handler executes a normalized command request.
type Handler interface {
	Run(context.Context, Request) (*Result, error)
}

// HandlerFunc adapts a function to the Handler interface.
type HandlerFunc func(context.Context, Request) (*Result, error)

// Run calls f with ctx and req.
func (f HandlerFunc) Run(ctx context.Context, req Request) (*Result, error) {
	return f(ctx, req)
}

// Renderer writes a command result to the configured output streams.
type Renderer interface {
	Render(context.Context, *Result) error
}

// RendererFunc adapts a function to the Renderer interface.
type RendererFunc func(context.Context, *Result) error

// Render calls f with ctx and result.
func (f RendererFunc) Render(ctx context.Context, result *Result) error {
	return f(ctx, result)
}

// Runtime bundles the executable handler and optional custom renderer.
type Runtime struct {
	Handler  Handler
	Renderer Renderer
}

// Deps is the runtime injection point. ControlPlane and DataPlane are explicit
// dependency slots; command modules decide which small interfaces they require.
type Deps struct {
	Config *config.Config
	IO     *iostreams.IOStreams
	// ControlPlane is intentionally untyped; command modules assert the small
	// interface they need so tests can inject focused fakes.
	ControlPlane any
	// DataPlane is the equivalent dependency slot for tunnels, adb, proxy, file
	// transfer, and remote execution workflows.
	DataPlane any
	Stdin     io.Reader
	Now       func() time.Time
	// Values is a small escape hatch for feature-specific dependencies that do
	// not justify a shared field on Deps.
	Values map[string]any
}

// WithDefaults fills unset runtime dependencies with process defaults.
func (d Deps) WithDefaults() Deps {
	if d.IO == nil {
		d.IO = iostreams.System()
	}
	if d.Config == nil {
		d.Config = config.Get()
	}
	if d.Stdin == nil && d.IO != nil {
		d.Stdin = d.IO.In
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return d
}

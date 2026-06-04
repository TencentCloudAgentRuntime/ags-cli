// Package command defines the registry-facing command model used by both
// generated API commands and hand-written workflow commands.
package command

import (
	"fmt"
	"sort"
	"strings"
)

// Registry stores command modules and group metadata keyed by stable command ID.
// It is deliberately metadata-first: callers can enumerate descriptors to build
// help, schemas, and diagnostics without constructing runtime clients.
type Registry struct {
	modulesByID    map[string]Module
	moduleOrder    []string
	pathsByKey     map[string]string
	groupsByPath   map[string]GroupSpec
	groupPathOrder []string
}

// NewRegistry creates an empty command registry.
func NewRegistry() *Registry {
	return &Registry{
		modulesByID:  map[string]Module{},
		pathsByKey:   map[string]string{},
		groupsByPath: map[string]GroupSpec{},
	}
}

// Register adds a command module after validating its ID, path, build hook, and
// path uniqueness. Group metadata embedded in the module is registered before
// the module so cmdtree can later attach it deterministically.
func (r *Registry) Register(module Module) error {
	if r == nil {
		return fmt.Errorf("command registry is nil")
	}
	spec := module.Descriptor.Spec
	if spec.ID == "" {
		return fmt.Errorf("command module missing spec id")
	}
	if len(spec.Path) == 0 {
		return fmt.Errorf("command module %q missing spec path", spec.ID)
	}
	if module.Build == nil {
		return fmt.Errorf("command module %q missing Build", spec.ID)
	}
	if _, exists := r.modulesByID[spec.ID]; exists {
		return fmt.Errorf("duplicate command id %q", spec.ID)
	}
	pathKey := pathKey(spec.Path)
	if existingID, exists := r.pathsByKey[pathKey]; exists {
		return fmt.Errorf("duplicate command path %q for %q and %q", pathKey, existingID, spec.ID)
	}
	for _, g := range module.Descriptor.Groups {
		if err := r.RegisterGroup(g); err != nil {
			return err
		}
	}
	r.modulesByID[spec.ID] = module
	r.moduleOrder = append(r.moduleOrder, spec.ID)
	r.pathsByKey[pathKey] = spec.ID
	return nil
}

// MustRegister adds a module and panics if the module is invalid.
func (r *Registry) MustRegister(module Module) {
	if err := r.Register(module); err != nil {
		panic(err)
	}
}

// RegisterGroup adds metadata for an intermediate command group. Re-registering
// identical group metadata is allowed because many leaf modules declare the same
// parent path; conflicting metadata is rejected to avoid unstable help output.
func (r *Registry) RegisterGroup(group GroupSpec) error {
	if r == nil {
		return fmt.Errorf("command registry is nil")
	}
	if len(group.Path) == 0 {
		return fmt.Errorf("group metadata missing path")
	}
	key := pathKey(group.Path)
	if existing, ok := r.groupsByPath[key]; ok {
		if !sameGroup(existing, group) {
			return fmt.Errorf("conflicting group metadata for %q", key)
		}
		return nil
	}
	r.groupsByPath[key] = group
	r.groupPathOrder = append(r.groupPathOrder, key)
	return nil
}

// Lookup returns the module registered for id.
func (r *Registry) Lookup(id string) (Module, bool) {
	if r == nil {
		return Module{}, false
	}
	m, ok := r.modulesByID[id]
	return m, ok
}

// Modules returns registered command modules in registration order.
func (r *Registry) Modules() []Module {
	if r == nil {
		return nil
	}
	out := make([]Module, 0, len(r.moduleOrder))
	for _, id := range r.moduleOrder {
		out = append(out, r.modulesByID[id])
	}
	return out
}

// Descriptors returns metadata-only command descriptions in registration order.
// It does not call Module.Build and does not require runtime dependencies.
func (r *Registry) Descriptors() []Descriptor {
	if r == nil {
		return nil
	}
	out := make([]Descriptor, 0, len(r.moduleOrder))
	for _, id := range r.moduleOrder {
		out = append(out, cloneDescriptor(r.modulesByID[id].Descriptor))
	}
	return out
}

// Groups returns group metadata sorted from shallow paths to deeper paths. The
// tree builder depends on parents being available before children.
func (r *Registry) Groups() []GroupSpec {
	if r == nil {
		return nil
	}
	keys := append([]string(nil), r.groupPathOrder...)
	sort.SliceStable(keys, func(i, j int) bool {
		ai := strings.Count(keys[i], ".")
		aj := strings.Count(keys[j], ".")
		if ai == aj {
			return keys[i] < keys[j]
		}
		return ai < aj
	})
	out := make([]GroupSpec, 0, len(keys))
	for _, key := range keys {
		out = append(out, r.groupsByPath[key])
	}
	return out
}

// Group returns metadata for the group at path.
func (r *Registry) Group(path []string) (GroupSpec, bool) {
	if r == nil {
		return GroupSpec{}, false
	}
	g, ok := r.groupsByPath[pathKey(path)]
	return g, ok
}

func pathKey(path []string) string {
	return strings.Join(path, ".")
}

func sameGroup(a, b GroupSpec) bool {
	return pathKey(a.Path) == pathKey(b.Path) &&
		a.Use == b.Use &&
		a.Short == b.Short &&
		a.Long == b.Long &&
		strings.Join(a.Aliases, "\x00") == strings.Join(b.Aliases, "\x00")
}

func cloneDescriptor(in Descriptor) Descriptor {
	out := in
	out.Spec = cloneSpec(in.Spec)
	out.Groups = cloneGroups(in.Groups)
	if in.Generated != nil {
		generated := cloneDescriptor(*in.Generated)
		out.Generated = &generated
	}
	return out
}

func cloneSpec(in Spec) Spec {
	out := in
	out.Path = append([]string(nil), in.Path...)
	out.Examples = append([]string(nil), in.Examples...)
	out.Aliases = append([]string(nil), in.Aliases...)
	out.Args = append([]ArgSpec(nil), in.Args...)
	out.Flags = make([]FlagSpec, len(in.Flags))
	for i, flag := range in.Flags {
		out.Flags[i] = flag
		out.Flags[i].Aliases = append([]string(nil), flag.Aliases...)
		out.Flags[i].Examples = append([]string(nil), flag.Examples...)
		out.Flags[i].Values = append([]string(nil), flag.Values...)
		if flag.Annotations != nil {
			out.Flags[i].Annotations = map[string][]string{}
			for key, values := range flag.Annotations {
				out.Flags[i].Annotations[key] = append([]string(nil), values...)
			}
		}
	}
	out.Output.Effects = append([]string(nil), in.Output.Effects...)
	return out
}

func cloneGroups(in []GroupSpec) []GroupSpec {
	out := make([]GroupSpec, len(in))
	for i, group := range in {
		out[i] = group
		out[i].Path = append([]string(nil), group.Path...)
		out[i].Aliases = append([]string(nil), group.Aliases...)
		out[i].Examples = append([]string(nil), group.Examples...)
	}
	return out
}

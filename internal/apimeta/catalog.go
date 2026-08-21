// Package apimeta is the API metadata centre. Runtime consumes generated
// Go metadata produced from the effective API, mapping.yaml and help.json; it does
// not read or embed a committed JSON catalog.
package apimeta

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

var (
	once  sync.Once
	cat   *Catalog
	loadE error
)

// Catalog is the runtime view of generated API metadata. It presents
// the persisted facts (Actions + Objects) to callers and also exposes
// Spec / Mapping adapters so existing helpers that expect those types
// keep working.
type Catalog struct {
	APIVersion string
	Actions    map[string]CatalogActionEntry
	Objects    map[string]CatalogObject

	// Spec / Mapping are runtime adapters reconstructed from Actions
	// + Objects. They give callers `cat.Spec.Object(name)` and
	// `cat.Mapping.Action(name)` without having to mirror api.json /
	// mapping.yaml at runtime (NextPlan §9.1).
	Spec    *Spec
	Mapping *Mapping

	// commandIndex maps "instance.create" -> "StartSandboxInstance".
	commandIndex map[string]string
}

// Get returns the singleton runtime catalog.
func Get() (*Catalog, error) {
	once.Do(func() {
		var file CatalogFile
		if err := json.Unmarshal(generatedCatalogJSON, &file); err != nil {
			loadE = fmt.Errorf("decode generated API metadata: %w", err)
			return
		}
		c := &Catalog{
			APIVersion:   file.APIVersion,
			Actions:      map[string]CatalogActionEntry{},
			Objects:      map[string]CatalogObject{},
			commandIndex: map[string]string{},
		}
		for _, a := range file.Actions {
			c.Actions[a.Action] = a
			if a.Status == StatusMapped && a.Command != "" {
				c.commandIndex[a.Command] = a.Action
			}
		}
		for name, obj := range file.Objects {
			c.Objects[name] = obj
		}
		c.Spec = newCatalogSpec(c)
		c.Mapping = newCatalogMapping(c, file.APIVersion)
		cat = c
	})
	if loadE != nil {
		return nil, loadE
	}
	return cat, nil
}

// newCatalogSpec rebuilds a *Spec view backed by the catalog. The
// returned object exposes the same methods as a freshly-loaded
// api.json so existing call sites (cat.Spec.Object, cat.Spec.SortedObjectNames,
// cat.Spec.Actions) keep compiling.
func newCatalogSpec(c *Catalog) *Spec {
	spec := &Spec{
		Actions: map[string]Action{},
		Objects: map[string]*Object{},
	}
	for name, a := range c.Actions {
		spec.Actions[name] = Action{
			Name:   name,
			Input:  a.Request,
			Output: a.Response,
		}
	}
	for name, obj := range c.Objects {
		o := &Object{Name: name, Document: obj.Document}
		for _, m := range obj.Members {
			o.Members = append(o.Members, Member{
				Name:     m.Name,
				Type:     m.Type,
				Member:   m.Member,
				Required: m.Required,
				Document: m.Document,
			})
		}
		spec.Objects[name] = o
	}
	return spec
}

// newCatalogMapping rebuilds a *Mapping view backed by the catalog.
func newCatalogMapping(c *Catalog, apiVersion string) *Mapping {
	m := &Mapping{
		APIVersion: apiVersion,
		Actions:    map[string]*ActionMapping{},
	}
	for name, a := range c.Actions {
		entry := &ActionMapping{
			Name:     name,
			Status:   a.Status,
			Request:  a.Request,
			Response: a.Response,
			Reason:   a.Reason,
			Command:  a.Command,
			Fields:   map[string]*FieldMapping{},
		}
		if a.CLI != nil {
			entry.Command = a.CLI.Command
			for fname, f := range a.CLI.Fields {
				entry.Fields[fname] = &FieldMapping{
					Flag:       f.Flag,
					Shorthand:  f.Shorthand,
					Aliases:    append([]string(nil), f.Aliases...),
					Parser:     f.Parser,
					Inputs:     mappingInputs(f.Inputs),
					Positional: f.Positional,
					Excluded:   f.Excluded,
				}
			}
		}
		m.Actions[name] = entry
	}
	return m
}

func mappingInputs(inputs []CatalogInputEntry) []InputMapping {
	if len(inputs) == 0 {
		return nil
	}
	out := make([]InputMapping, 0, len(inputs))
	for _, in := range inputs {
		out = append(out, InputMapping{
			Flag:      in.Flag,
			Type:      in.Type,
			Form:      in.Form,
			Default:   in.Default,
			Shorthand: in.Shorthand,
			Aliases:   append([]string(nil), in.Aliases...),
		})
	}
	return out
}

// Action returns the catalog entry for the given API action name.
func (c *Catalog) Action(name string) (CatalogActionEntry, bool) {
	a, ok := c.Actions[name]
	return a, ok
}

// Object returns the catalog entry for the given object name.
func (c *Catalog) Object(name string) (CatalogObject, bool) {
	o, ok := c.Objects[name]
	return o, ok
}

// SortedActionNames returns action names in deterministic order.
func (c *Catalog) SortedActionNames() []string {
	names := make([]string, 0, len(c.Actions))
	for n := range c.Actions {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// SortedObjectNames returns object names in deterministic order.
func (c *Catalog) SortedObjectNames() []string {
	names := make([]string, 0, len(c.Objects))
	for n := range c.Objects {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// MappedActionNames returns only mapped actions, sorted.
func (c *Catalog) MappedActionNames() []string {
	var out []string
	for _, n := range c.SortedActionNames() {
		if c.Actions[n].Status == StatusMapped {
			out = append(out, n)
		}
	}
	return out
}

// ActionForCommand returns the api action mapped to the given resource
// command (e.g. "instance.create" -> "StartSandboxInstance").
func ActionForCommand(command string) (string, bool) {
	c, err := Get()
	if err != nil {
		return "", false
	}
	a, ok := c.commandIndex[command]
	return a, ok
}

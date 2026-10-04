// Package tools defines what the agent can do besides talk.
//
// A tool is four things: a name, a description the model reads to decide when
// to use it, a JSON schema for its input, and a Go function. The model never
// runs anything. It asks, by name, with JSON; this package runs the function
// and the answer goes back as text.
package tools

import (
	"context"
	"encoding/json"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/invopop/jsonschema"

	"github.com/aurimas13/Code-Editing-Agent/internal/workspace"
)

// Tool is one capability offered to the model.
type Tool struct {
	Name        string
	Description string
	InputSchema anthropic.ToolInputSchemaParam
	// Mutates marks tools that change the workspace. The agent asks its
	// approver before running these.
	Mutates bool
	// Run executes the tool against a workspace. A returned error is sent
	// back to the model as an error result; it does not end the turn.
	Run func(ctx context.Context, fs workspace.FS, input json.RawMessage) (string, error)
}

// Registry is an ordered, name-indexed set of tools.
type Registry struct {
	tools  []Tool
	byName map[string]int
}

// NewRegistry builds a registry. Later tools replace earlier ones of the same
// name.
func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{byName: map[string]int{}}
	for _, t := range tools {
		if i, ok := r.byName[t.Name]; ok {
			r.tools[i] = t
			continue
		}
		r.byName[t.Name] = len(r.tools)
		r.tools = append(r.tools, t)
	}
	return r
}

// Default returns the three tools from the tutorial: read, list, edit.
func Default() *Registry {
	return NewRegistry(ReadFile, ListFiles, EditFile)
}

// Get looks a tool up by the name the model used.
func (r *Registry) Get(name string) (Tool, bool) {
	i, ok := r.byName[name]
	if !ok {
		return Tool{}, false
	}
	return r.tools[i], true
}

// Names lists the registered tools in order.
func (r *Registry) Names() []string {
	out := make([]string, len(r.tools))
	for i, t := range r.tools {
		out[i] = t.Name
	}
	return out
}

// Params converts the registry into the shape the Messages API expects.
func (r *Registry) Params() []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, anthropic.ToolUnionParam{OfTool: &anthropic.ToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: t.InputSchema,
		}})
	}
	return out
}

// GenerateSchema derives a tool's JSON schema from a Go struct, so the schema
// the model sees and the struct the tool decodes into cannot drift apart.
// Fields without `omitempty` are reported as required.
func GenerateSchema[T any]() anthropic.ToolInputSchemaParam {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v T
	schema := reflector.Reflect(v)
	return anthropic.ToolInputSchemaParam{
		Properties: schema.Properties,
		Required:   schema.Required,
	}
}

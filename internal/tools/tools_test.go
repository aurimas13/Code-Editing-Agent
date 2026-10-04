package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aurimas13/Code-Editing-Agent/internal/workspace"
)

func TestSchemasMarkRequiredFields(t *testing.T) {
	// The tutorial's schema omitted "required", so the model was never told
	// which parameters it had to send.
	cases := map[string][]string{
		"read_file":  {"path"},
		"list_files": nil, // path is optional
		"edit_file":  {"path", "old_str", "new_str"},
	}
	for name, want := range cases {
		tool, ok := Default().Get(name)
		if !ok {
			t.Fatalf("missing tool %s", name)
		}
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Type       string         `json:"type"`
			Properties map[string]any `json:"properties"`
			Required   []string       `json:"required"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		if schema.Type != "object" || strings.Join(schema.Required, ",") != strings.Join(want, ",") {
			t.Errorf("%s: type=%q required=%v, want object %v", name, schema.Type, schema.Required, want)
		}
		for _, field := range want {
			if _, ok := schema.Properties[field]; !ok {
				t.Errorf("%s: schema has no property %q", name, field)
			}
		}
	}
}

func TestOnlyEditFileMutates(t *testing.T) {
	for _, name := range Default().Names() {
		tool, _ := Default().Get(name)
		if tool.Mutates != (name == "edit_file") {
			t.Errorf("%s: Mutates = %v", name, tool.Mutates)
		}
	}
}

func TestReadFileRejectsBinary(t *testing.T) {
	fs, _ := workspace.NewMemFS(workspace.DemoLimits, map[string]string{"blob.bin": "PK\x03\x04\x00\x00"})
	_, err := ReadFile.Run(context.Background(), fs, json.RawMessage(`{"path":"blob.bin"}`))
	if err == nil || !strings.Contains(err.Error(), "not a text file") {
		t.Errorf("got %v, want a not-a-text-file error", err)
	}
}

func TestRegistryParamsAreValidAPIInput(t *testing.T) {
	raw, err := json.Marshal(Default().Params())
	if err != nil {
		t.Fatal(err)
	}
	var params []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		InputSchema struct {
			Type string `json:"type"`
		} `json:"input_schema"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatal(err)
	}
	if len(params) != 3 {
		t.Fatalf("got %d tools", len(params))
	}
	for _, p := range params {
		if p.Name == "" || p.Description == "" || p.InputSchema.Type != "object" {
			t.Errorf("incomplete tool definition: %+v", p)
		}
	}
}

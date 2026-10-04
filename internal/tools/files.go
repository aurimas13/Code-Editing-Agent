package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/aurimas13/Code-Editing-Agent/internal/workspace"
)

// ---- read_file ------------------------------------------------------------

type ReadFileInput struct {
	Path string `json:"path" jsonschema_description:"The relative path of a file in the working directory."`
}

var ReadFile = Tool{
	Name:        "read_file",
	Description: "Read the contents of a given relative file path. Use this when you want to see what's inside a file. Do not use this with directory names.",
	InputSchema: GenerateSchema[ReadFileInput](),
	Run: func(_ context.Context, fs workspace.FS, raw json.RawMessage) (string, error) {
		var in ReadFileInput
		if err := decode(raw, &in); err != nil {
			return "", err
		}
		data, err := fs.ReadFile(in.Path)
		if err != nil {
			return "", err
		}
		if !isText(data) {
			return "", fmt.Errorf("%s is not a text file (%d bytes)", in.Path, len(data))
		}
		return string(data), nil
	},
}

// ---- list_files -----------------------------------------------------------

type ListFilesInput struct {
	Path string `json:"path,omitempty" jsonschema_description:"Optional relative path to list files from. Defaults to current directory if not provided."`
}

var ListFiles = Tool{
	Name:        "list_files",
	Description: "List files and directories at a given path. If no path is provided, lists files in the current directory. Directories end with a slash.",
	InputSchema: GenerateSchema[ListFilesInput](),
	Run: func(_ context.Context, fs workspace.FS, raw json.RawMessage) (string, error) {
		var in ListFilesInput
		if err := decode(raw, &in); err != nil {
			return "", err
		}
		entries, err := fs.List(in.Path)
		if err != nil {
			return "", err
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.Dir {
				names = append(names, e.Path+"/")
			} else {
				names = append(names, e.Path)
			}
		}
		out, err := json.Marshal(names)
		if err != nil {
			return "", err
		}
		return string(out), nil
	},
}

// ---- edit_file ------------------------------------------------------------

type EditFileInput struct {
	Path   string `json:"path" jsonschema_description:"The relative path to the file."`
	OldStr string `json:"old_str" jsonschema_description:"Text to search for. It must match exactly and must appear exactly once in the file. Use an empty string only to create a new file."`
	NewStr string `json:"new_str" jsonschema_description:"Text to replace old_str with."`
}

var EditFile = Tool{
	Name: "edit_file",
	Description: `Make edits to a text file.

Replaces 'old_str' with 'new_str' in the given file. 'old_str' and 'new_str' MUST be different from each other, and 'old_str' must appear exactly once in the file; include enough surrounding text to make it unique.

If the file specified with path doesn't exist and 'old_str' is empty, the file is created with 'new_str' as its content.`,
	InputSchema: GenerateSchema[EditFileInput](),
	Mutates:     true,
	Run: func(_ context.Context, fs workspace.FS, raw json.RawMessage) (string, error) {
		var in EditFileInput
		if err := decode(raw, &in); err != nil {
			return "", err
		}
		if in.Path == "" {
			return "", errors.New("path is required")
		}
		if in.OldStr == in.NewStr {
			return "", errors.New("old_str and new_str are identical; nothing to change")
		}

		data, err := fs.ReadFile(in.Path)
		switch {
		case errors.Is(err, workspace.ErrNotFound):
			if in.OldStr != "" {
				return "", fmt.Errorf("%s does not exist; to create it, call edit_file with an empty old_str", in.Path)
			}
			if err := fs.WriteFile(in.Path, []byte(in.NewStr)); err != nil {
				return "", err
			}
			return fmt.Sprintf("Created %s (%d bytes)", in.Path, len(in.NewStr)), nil
		case err != nil:
			return "", err
		}

		if !isText(data) {
			return "", fmt.Errorf("%s is not a text file", in.Path)
		}
		content := string(data)

		// The tutorial used strings.Replace(content, old, new, -1). With an
		// empty old_str on an existing file, that inserts new_str between
		// every character. With a non-unique old_str, it silently rewrites
		// every match. Both are refused here, with a message that tells the
		// model how to recover.
		if in.OldStr == "" {
			if content != "" {
				return "", fmt.Errorf("%s already exists; read it and pass the exact text to replace as old_str", in.Path)
			}
		} else {
			switch n := strings.Count(content, in.OldStr); {
			case n == 0:
				return "", fmt.Errorf("old_str not found in %s; read the file and copy the text exactly, including whitespace", in.Path)
			case n > 1:
				return "", fmt.Errorf("old_str appears %d times in %s; include more surrounding text so it matches exactly once", n, in.Path)
			}
		}

		updated := strings.Replace(content, in.OldStr, in.NewStr, 1)
		if err := fs.WriteFile(in.Path, []byte(updated)); err != nil {
			return "", err
		}
		return fmt.Sprintf("Edited %s (1 replacement, %d bytes)", in.Path, len(updated)), nil
	},
}

// ---- helpers --------------------------------------------------------------

// decode parses tool input strictly: unknown fields are an error, so a model
// that invents a parameter is told so instead of being silently ignored.
func decode(raw json.RawMessage, v any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`{}`)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid tool input: %w", err)
	}
	return nil
}

// isText reports whether data is valid UTF-8 without NUL bytes.
func isText(data []byte) bool {
	return utf8.Valid(data) && bytes.IndexByte(data, 0) < 0
}

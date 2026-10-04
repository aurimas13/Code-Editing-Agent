// Package guide embeds the six checkpoints of the tutorial build.
//
// Each directory under steps/ is a complete, runnable program: the agent as
// it stands at the end of that step. The website's step-by-step guide is
// generated from these files, and CI compiles every one of them, so the
// guide cannot show code that does not build.
package guide

import (
	"embed"
	"io/fs"
	"sort"
)

//go:embed steps/*/main.go
var files embed.FS

// Step is one checkpoint.
type Step struct {
	ID   string `json:"id"`   // directory name, e.g. "03-read-file"
	Code string `json:"code"` // full contents of main.go at this step
}

// Steps returns every checkpoint in order.
func Steps() []Step {
	dirs, err := fs.ReadDir(files, "steps")
	if err != nil {
		panic(err) // embedded at build time; cannot fail at run time
	}
	var out []Step
	for _, d := range dirs {
		code, err := files.ReadFile("steps/" + d.Name() + "/main.go")
		if err != nil {
			panic(err)
		}
		out = append(out, Step{ID: d.Name(), Code: string(code)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Final returns the finished tutorial program.
func Final() string {
	steps := Steps()
	return steps[len(steps)-1].Code
}

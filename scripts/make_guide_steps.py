#!/usr/bin/env python3
"""Derive the six tutorial checkpoints from the finished main.go.

The finished file (guide/steps/06-edit-file/main.go) is the source of truth.
Each earlier step is that file with later pieces removed, so the checkpoints
cannot drift apart. Run from the repository root; CI checks the output is
committed and that every step compiles.
"""
import pathlib, re, subprocess, sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
STEPS = ROOT / "guide" / "steps"
final = (STEPS / "06-edit-file" / "main.go").read_text()


def between(text, start, end=None):
    a = text.index(start)
    b = text.index(end, a) if end else len(text)
    return text[a:b]


# ---- pieces of the finished file -------------------------------------------
run_full = between(final, "func (a *Agent) Run(", "func (a *Agent) executeTool(")
execute_tool = between(final, "func (a *Agent) executeTool(", "func (a *Agent) runInference(")
run_inference_full = between(final, "func (a *Agent) runInference(", "type ToolDefinition struct")
tool_definition = between(final, "type ToolDefinition struct", "var ReadFileDefinition")
read_file = between(final, "var ReadFileDefinition", "func GenerateSchema")
generate_schema = between(final, "func GenerateSchema", "var ListFilesDefinition")
list_files = between(final, "var ListFilesDefinition", "var EditFileDefinition")
edit_file = between(final, "var EditFileDefinition")

you_line = next(l for l in run_full.splitlines() if "You" in l and "fmt.Print" in l).strip()
claude_line = next(l for l in run_full.splitlines() if "Claude" in l and "fmt.Printf" in l).strip()

run_simple = f'''func (a *Agent) Run(ctx context.Context) error {{
	conversation := []anthropic.MessageParam{{}}

	fmt.Println("Chat with Claude (use 'ctrl-c' to quit)")

	for {{
		{you_line}
		userInput, ok := a.getUserMessage()
		if !ok {{
			break
		}}

		userMessage := anthropic.NewUserMessage(anthropic.NewTextBlock(userInput))
		conversation = append(conversation, userMessage)

		message, err := a.runInference(ctx, conversation)
		if err != nil {{
			return err
		}}
		conversation = append(conversation, message.ToParam())

		for _, content := range message.Content {{
			switch content.Type {{
			case "text":
				{claude_line}
			}}
		}}
	}}

	return nil
}}

'''

run_inference_simple = '''func (a *Agent) runInference(ctx context.Context, conversation []anthropic.MessageParam) (*anthropic.Message, error) {
	message, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     "claude-opus-5",
		MaxTokens: 16000,
		Messages:  conversation,
	})
	return message, err
}
'''

agent_plain = '''type Agent struct {
	client         *anthropic.Client
	getUserMessage func() (string, bool)
}

func NewAgent(client *anthropic.Client, getUserMessage func() (string, bool)) *Agent {
	return &Agent{
		client:         client,
		getUserMessage: getUserMessage,
	}
}

'''
agent_tools = between(final, "type Agent struct", "func main()")


def header(imports):
    std = [i for i in imports if "." not in i.split("/")[0]]
    ext = [i for i in imports if i not in std]
    lines = ["package main", "", "import ("]
    lines += [f'\t"{i}"' for i in sorted(std)]
    lines += [""] + [f'\t"{i}"' for i in sorted(ext)]
    lines += [")", "", ""]
    return "\n".join(lines)


def main_fn(tools):
    scanner = '''	client := anthropic.NewClient()

	scanner := bufio.NewScanner(os.Stdin)
	getUserMessage := func() (string, bool) {
		if !scanner.Scan() {
			return "", false
		}
		return scanner.Text(), true
	}

'''
    if tools is None:
        wiring = "	agent := NewAgent(&client, getUserMessage)\n"
    else:
        wiring = f"	tools := []ToolDefinition{{{', '.join(tools)}}}\n	agent := NewAgent(&client, getUserMessage, tools)\n"
    tail = '''	err := agent.Run(context.TODO())
	if err != nil {
		fmt.Printf("Error: %s\\n", err.Error())
	}
}

'''
    return "func main() {\n" + scanner + wiring + tail


SDK = "github.com/anthropics/anthropic-sdk-go"
SCHEMA = "github.com/invopop/jsonschema"
base = ["bufio", "context", "fmt", "os", SDK]

steps = {
    "01-chat": header(base) + agent_plain + main_fn(None) + run_simple + run_inference_simple,
    "02-tool-definitions": header(base + ["encoding/json"]) + agent_tools + main_fn([]) + run_simple
    + run_inference_full + tool_definition.rstrip() + "\n",
    "03-read-file": header(base + ["encoding/json", SCHEMA]) + agent_tools + main_fn(["ReadFileDefinition"])
    + run_simple + run_inference_full + tool_definition + read_file + generate_schema.rstrip() + "\n",
    "04-tool-loop": header(base + ["encoding/json", SCHEMA]) + agent_tools + main_fn(["ReadFileDefinition"])
    + run_full + execute_tool + run_inference_full + tool_definition + read_file + generate_schema.rstrip() + "\n",
    "05-list-files": header(base + ["encoding/json", "path/filepath", SCHEMA]) + agent_tools
    + main_fn(["ReadFileDefinition", "ListFilesDefinition"]) + run_full + execute_tool + run_inference_full
    + tool_definition + read_file + generate_schema + list_files.rstrip() + "\n",
}

for name, src in steps.items():
    path = STEPS / name / "main.go"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(src)
    subprocess.run(["gofmt", "-w", str(path)], check=True)
    print("wrote", path.relative_to(ROOT))

package fakellm

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// DemoBrain is a rule-based responder that stands in for the model when no
// API key is configured. It is not intelligent and does not pretend to be:
// it recognises the handful of requests the tutorial demonstrates, drives
// the real tools through the real loop, and says what it is when asked
// anything else. Its purpose is to let someone clone the repository and see
// the loop work in a minute, with nothing to sign up for.
func DemoBrain(req Request) Response {
	task, steps := currentTurn(req)
	lower := strings.ToLower(task)
	n := len(steps)

	last := step{}
	if n > 0 {
		last = steps[n-1]
	}
	call := func(name string, input any) Response {
		raw, _ := json.Marshal(input)
		return ToolUse(fmt.Sprintf("toolu_demo_%d", n+1), name, string(raw))
	}

	switch {
	// Anything that tries to leave the workspace: attempt it, so the visitor
	// sees the sandbox refuse.
	case strings.Contains(task, "../") || strings.Contains(lower, "/etc/") || strings.Contains(lower, "~/"):
		if n == 0 {
			return call("read_file", map[string]string{"path": firstPathLike(task)})
		}
		return say("I tried, and the workspace refused: "+last.result+"\n\nThe tools can only see files inside this sandbox. That check lives in the Go code, not in my instructions, so asking nicely does not get around it.",
			"I called read_file with the path you gave; the workspace rejected it before any file was opened.")

	case strings.Contains(lower, "secret-file") || strings.Contains(lower, "riddle"):
		if n == 0 {
			return call("read_file", map[string]string{"path": "secret-file.txt"})
		}
		if last.isError {
			return say("I couldn't read secret-file.txt: "+last.result, "I called read_file and it returned an error.")
		}
		return say("secret-file.txt holds a riddle:\n\n> "+strings.TrimSpace(last.result)+"\n\nThe answer is a horse.",
			"I called read_file on secret-file.txt, then answered from what came back.")

	case strings.Contains(lower, "bug") || (strings.Contains(lower, "fix") && strings.Contains(lower, "greet")):
		switch n {
		case 0:
			return call("read_file", map[string]string{"path": "greet.js"})
		case 1:
			if last.isError || !strings.Contains(last.result, "nmae") {
				return say("I read greet.js and could not find the typo I know how to fix. It may already be fixed.", "I called read_file on greet.js.")
			}
			return call("edit_file", map[string]string{"path": "greet.js", "old_str": `"Hello, " + nmae`, "new_str": `"Hello, " + name`})
		default:
			return say("Fixed. greet.js used a variable called `nmae` that was never defined; it now uses the `name` parameter. Press Run on the file to see it print the greeting.",
				"read_file to see the code, then edit_file to replace the one wrong word.")
		}

	case strings.Contains(lower, "fizzbuzz"):
		limit := firstNumber(task)
		wantsEdit := regexp.MustCompile(`\b(edit|change|update|only|until|up to)\b`).MatchString(lower)
		switch {
		case n == 0:
			return call("read_file", map[string]string{"path": "fizzbuzz.js"})
		case n == 1 && last.isError:
			if limit == "" {
				limit = "100"
			}
			return call("edit_file", map[string]string{"path": "fizzbuzz.js", "old_str": "", "new_str": fizzbuzzSource(limit)})
		case n == 1 && wantsEdit && limit != "":
			m := regexp.MustCompile(`limit = (\d+)`).FindStringSubmatch(last.result)
			if m == nil || m[1] == limit {
				return say("fizzbuzz.js already prints up to "+limit+".", "I called read_file and saw nothing to change.")
			}
			return call("edit_file", map[string]string{"path": "fizzbuzz.js", "old_str": "limit = " + m[1], "new_str": "limit = " + limit})
		case n == 1:
			return say("fizzbuzz.js already exists. Ask me to change how far it counts, or press Run to see its output.", "I called read_file to check before creating anything.")
		default:
			if last.isError {
				return say("The edit failed: "+last.result, "read_file, then edit_file, which returned an error.")
			}
			return say("Done. "+last.result+". Open fizzbuzz.js in the file panel and press Run to see the output.",
				"read_file to check whether the file existed, then edit_file to write it.")
		}

	case strings.Contains(lower, "congrats") || strings.Contains(lower, "rot13"):
		if n == 0 {
			return call("edit_file", map[string]string{"path": "congrats.js", "old_str": "", "new_str": congratsSource})
		}
		if last.isError {
			return say("I couldn't create congrats.js: "+last.result, "edit_file returned an error.")
		}
		return say("Created congrats.js. It ROT13-decodes a string and prints it. Press Run on the file to read the message.",
			"One edit_file call with an empty old_str, which creates a new file.")

	case regexp.MustCompile(`\b(list|what do you see|which files|what files|directory|folder)\b`).MatchString(lower):
		if n == 0 {
			return call("list_files", map[string]string{})
		}
		var names []string
		_ = json.Unmarshal([]byte(last.result), &names)
		return say(fmt.Sprintf("This workspace has %d entries:\n\n- %s", len(names), strings.Join(names, "\n- ")),
			"One list_files call; the answer is the list it returned.")

	case fileMention(task) != "":
		name := fileMention(task)
		if n == 0 {
			return call("read_file", map[string]string{"path": name})
		}
		if last.isError {
			return say("I couldn't read "+name+": "+last.result, "read_file returned an error.")
		}
		lines := strings.Count(last.result, "\n") + 1
		return say(fmt.Sprintf("%s is %d lines long. It begins:\n\n```\n%s\n```", name, lines, head(last.result, 12)),
			"One read_file call. A real model would summarise the file; this scripted stand-in can only quote it.")
	}

	return say("This site is running in demo mode: there is no API key configured, so a small scripted stand-in is answering instead of Claude. It can follow the tutorial's examples:\n\n- What's in secret-file.txt?\n- What do you see in this directory?\n- Find and fix the bug in greet.js\n- Create fizzbuzz.js that prints up to 30\n- Create a congrats.js script that rot13-decodes a message\n- Read ../../etc/passwd (to watch the sandbox refuse)\n\nSet ANTHROPIC_API_KEY on the server to talk to the real model.", "")
}

// step is one tool call of the current turn and what came back.
type step struct {
	name    string
	result  string
	isError bool
}

// currentTurn finds the latest user text and the tool calls made since.
func currentTurn(req Request) (task string, steps []step) {
	start := -1
	for i := len(req.Messages) - 1; i >= 0; i-- {
		m := req.Messages[i]
		if m.Role != "user" {
			continue
		}
		for _, b := range m.Content {
			if b.Type == "text" {
				task, start = b.Text, i
			}
		}
		if start >= 0 {
			break
		}
	}
	if start < 0 {
		return "", nil
	}
	names := map[string]string{}
	for _, m := range req.Messages[start+1:] {
		for _, b := range m.Content {
			switch b.Type {
			case "tool_use":
				names[b.ID] = b.Name
			case "tool_result":
				steps = append(steps, step{name: names[b.ToolUseID], result: b.ResultText(), isError: b.IsError})
			}
		}
	}
	return task, steps
}

func say(text, how string) Response {
	if how != "" {
		text += "\n\nHow I did it: " + how
	}
	return Text(text)
}

var (
	fileRe   = regexp.MustCompile(`[\w./\-]+\.(?:go|js|ts|txt|md|mod|json|py|html|css)\b`)
	pathRe   = regexp.MustCompile(`[~./][\w./\-~]+`)
	numberRe = regexp.MustCompile(`\b\d{1,6}\b`)
)

func fileMention(s string) string { return fileRe.FindString(s) }
func firstNumber(s string) string { return numberRe.FindString(s) }
func firstPathLike(s string) string {
	if p := pathRe.FindString(s); p != "" {
		return p
	}
	return "../"
}

func head(s string, lines int) string {
	parts := strings.SplitN(s, "\n", lines+1)
	if len(parts) > lines {
		parts = append(parts[:lines], "…")
	}
	return strings.Join(parts, "\n")
}

func fizzbuzzSource(limit string) string {
	return `function fizzbuzz(n) {
  if (n % 15 === 0) return "FizzBuzz";
  if (n % 3 === 0) return "Fizz";
  if (n % 5 === 0) return "Buzz";
  return String(n);
}

function run(limit = ` + limit + `) {
  for (let i = 1; i <= limit; i++) {
    console.log(fizzbuzz(i));
  }
}

run();
`
}

const congratsSource = `function rot13(str) {
  return str.replace(/[a-zA-Z]/g, (char) => {
    const base = char <= 'Z' ? 65 : 97;
    return String.fromCharCode(((char.charCodeAt(0) - base + 13) % 26) + base);
  });
}

const encoded = 'Pbatenghyngvbaf ba ohvyqvat n pbqr-rqvgvat ntrag!';
console.log(rot13(encoded));
`

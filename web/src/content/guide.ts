// The words of the build guide. The code shown beside each step is not
// written here: it comes from guide/steps/*/main.go, which CI compiles.

export type GuideStep = {
  /** Matches a directory in guide/steps, or has no checkpoint (setup, production). */
  id: string;
  /** Checkpoint directory this step's code comes from, if any. */
  checkpoint?: string;
  nav: string;
  title: string;
  /** How much of the loop diagram exists after this step (1 to 7). */
  stage: number;
  eli5: string[];
  /** What you type, in order. */
  adds: string[];
  commands?: string[];
  tryIt?: { say: string; see: string };
  /** One thing that commonly goes wrong at this step. */
  watchOut?: string;
};

export const guideSteps: GuideStep[] = [
  {
    id: "00-setup",
    nav: "Set up",
    title: "Make an empty project",
    stage: 0,
    eli5: [
      "Before you can build with blocks you need a table to build on. These four commands make the table: a folder, a file that tells Go this folder is a project, an empty main.go to write in, and your key so Claude knows the bill goes to you.",
    ],
    adds: [
      "Nothing in main.go yet. You only run commands in your terminal.",
      "The folder name does not matter. The module name after `go mod init` does not matter either; the tutorial uses `agent`.",
    ],
    commands: [
      "mkdir code-editing-agent",
      "cd code-editing-agent",
      "go mod init agent",
      "touch main.go",
      'export ANTHROPIC_API_KEY="paste your key here"',
    ],
    watchOut:
      "The `export` line only lasts for the terminal window you typed it in. If you open a new window, type it again.",
  },
  {
    id: "01-chat",
    checkpoint: "01-chat",
    nav: "Chat",
    title: "Talk to Claude from your terminal",
    stage: 1,
    eli5: [
      "Imagine passing notes to a very clever friend who forgets everything the moment they hand a note back. So each time, you give them the whole pile of notes so far, and they read it from the top.",
      "That pile is the variable called `conversation`. The program does five things over and over: read what you typed, put it on the pile, send the pile to Claude, print the reply, put the reply on the pile.",
    ],
    adds: [
      "The whole file is new, so paste all of it into the empty main.go.",
      "`main` sets things up. `Run` is the loop. `runInference` is the one line that actually calls Claude.",
    ],
    commands: ["go mod tidy", "go run main.go"],
    tryIt: {
      say: "Hey! I'm learning to build an agent. How are you?",
      see: "A friendly reply. Ask a follow-up and it remembers your first message, because the whole pile went along.",
    },
    watchOut:
      "`go mod tidy` downloads the Anthropic library. If `go run` says a package is missing, you skipped it.",
  },
  {
    id: "02-tool-definitions",
    checkpoint: "02-tool-definitions",
    nav: "Tool menu",
    title: "Give Claude a menu of favours",
    stage: 2,
    eli5: [
      "Your friend is on the phone. They can't touch anything in your room, but you can do favours for them. First they need a menu: the name of each favour, when to ask for it, and what details you need.",
      "`ToolDefinition` is one line on that menu. In this step you only make room for the menu and send it along with every message. The menu is still empty and nobody does any favours yet.",
    ],
    adds: [
      "A new `tools` field on the Agent, and a third argument to `NewAgent` so the field gets filled in.",
      "In `main`, an empty list of tools that is handed to `NewAgent`.",
      "In `runInference`, a few lines that turn our list into the shape Claude's API wants, and one new line that sends it.",
      "At the bottom of the file, the `ToolDefinition` type itself.",
    ],
    commands: ["go run main.go"],
    tryIt: {
      say: "What tools do you have?",
      see: "It says it has none. The menu is being sent; it is just empty.",
    },
    watchOut:
      "This step adds `encoding/json` to the import list. Forget it and Go will say `undefined: json`.",
  },
  {
    id: "03-read-file",
    checkpoint: "03-read-file",
    nav: "read_file",
    title: "Write the first tool: read_file",
    stage: 3,
    eli5: [
      "Now you write the first favour on the menu: “read me a file”. A favour has three parts. The menu line (its name and when to use it). The form to fill in (here it has one blank: the path). And the Go function that really opens the file.",
      "Run it and ask about a file. Claude will now ask for the favour, but your program does not know what to do with the request yet, so nothing comes back. That feels broken. It is not. It is the next step.",
    ],
    adds: [
      "`ReadFileDefinition` plus its input form and its function, all below `ToolDefinition`. It needs a new import, `github.com/invopop/jsonschema`, so run `go mod tidy` again.",
      "`GenerateSchema`, a helper that builds the form description from a Go struct so you never write JSON by hand.",
      "One word inside `main`: `ReadFileDefinition` goes into the list of tools.",
    ],
    commands: ["go mod tidy", "go run main.go"],
    tryIt: {
      say: "What's in main.go?",
      see: "Claude says something like “I'll read that file”, then silence. It asked for read_file and nobody answered.",
    },
    watchOut:
      "Stop the program with ctrl-c after that one question. If you send a second message, the API returns an error, because Claude is still waiting for the result of the tool it asked for.",
  },
  {
    id: "04-tool-loop",
    checkpoint: "04-tool-loop",
    nav: "The loop",
    title: "Do the favour and report back",
    stage: 4,
    eli5: [
      "This is the step that turns a chatbot into an agent. When your friend says “please read main.go for me”, you don't wait for more typing. You go and read it, then tell them what it said. They might ask for another favour, or give you the final answer.",
      "So `Run` now looks at every reply and asks: is this words, or a favour? Words get printed. A favour gets done by `executeTool`, the result goes on the pile, and Claude gets another go without waiting for you. That is the loop.",
    ],
    adds: [
      "Inside `Run`: a flag called `readUserInput`, so the program can skip asking you when Claude is waiting for a tool result.",
      "Inside `Run`: a second `case` that catches `tool_use` and collects the results.",
      "A new method, `executeTool`, placed between `Run` and `runInference`. It finds the tool by name and calls its function.",
    ],
    commands: [
      "echo 'what animal is the most disagreeable because it always says neigh?' > secret-file.txt",
      "go run main.go",
    ],
    tryIt: {
      say: "Help me solve the riddle in secret-file.txt",
      see: "A green `tool:` line as your code reads the file, then Claude answers: a horse.",
    },
    watchOut:
      "The changes to `Run` are small but spread out. Use the highlighted lines on the right; if one is missing, the loop either never calls the tool or never stops.",
  },
  {
    id: "05-list-files",
    checkpoint: "05-list-files",
    nav: "list_files",
    title: "Let it look around: list_files",
    stage: 5,
    eli5: [
      "Your friend can read a page if they know its name. But they can't see your desk. `list_files` is them asking “what's on the desk?”.",
      "Look at what you did not have to change: the loop. Adding a tool is now always the same four small things: a menu line, a form, a function, and one word in the list.",
    ],
    adds: [
      "`ListFilesDefinition`, its input form and its function, at the bottom of the file.",
      "One word inside `main`: `ListFilesDefinition` joins the list of tools.",
      "One more import: `path/filepath`.",
    ],
    commands: ["go run main.go"],
    tryIt: {
      say: "What do you see in this directory?",
      see: "Claude calls list_files on its own, then describes your files. Ask “what Go version are we using?” and watch it list, then read go.mod.",
    },
  },
  {
    id: "06-edit-file",
    checkpoint: "06-edit-file",
    nav: "edit_file",
    title: "Let it change things: edit_file",
    stage: 6,
    eli5: [
      "The last favour: “find this exact sentence and swap it for that one”. That is all editing is here. Look for a piece of text, replace it. If the file is not there and there is nothing to look for, make a new file.",
      "With read, list and edit, your friend can explore a project and change it. That is a code-editing agent, in about three hundred lines.",
    ],
    adds: [
      "`EditFileDefinition`, its input form with three blanks (path, old text, new text), and its function.",
      "A small helper, `createNewFile`, at the very end.",
      "One word inside `main`, and two imports: `path` and `strings`.",
    ],
    commands: ["go run main.go", "node fizzbuzz.js"],
    tryIt: {
      say: "Create fizzbuzz.js that I can run with Node.js",
      see: "A new file appears in your folder. Run it with node. Then ask: “edit fizzbuzz.js so it only prints until 15”.",
    },
    watchOut:
      "This version will read and write any path on your computer that you can. Run it in a folder you don't mind it changing. The next step is about exactly that.",
  },
  {
    id: "07-production",
    nav: "Make it safe",
    title: "Before strangers use it",
    stage: 7,
    eli5: [
      "You would not hand your house keys to everyone on the internet. The tutorial agent can open any door on the computer it runs on, and it never gets tired or stops spending.",
      "The version running on this site keeps the same loop and puts it in a playpen. It can only touch the toys inside. There is a limit on how long it can play and how much it can cost. And everything it does is written down, so someone can check.",
    ],
    adds: [
      "The single file is split into small packages: the loop, the tools, the workspace, the limits, the storage.",
      "Every path goes through one checker, and files live in a per-visitor sandbox.",
      "edit_file refuses the two inputs that silently damaged files in the tutorial version.",
      "Rounds, tokens, time and daily spend are all capped. Each step is streamed, stored and tested.",
    ],
  },
];

/** What changed between the tutorial file and the deployed agent, and the evidence for each change. */
export const productionChanges: {
  area: string;
  tutorial: string;
  now: string;
  where: string;
  evidence: string;
}[] = [
  {
    area: "File access",
    tutorial: "The model picks any path. `../../etc/passwd` and `~/.ssh/id_rsa` both work.",
    now: "Paths are validated in one place and resolved inside a sandbox: an in-memory workspace per visitor on the web, an os.Root directory in the terminal.",
    where: "internal/workspace",
    evidence: "the sandbox evals; TestDiskFSConfinement",
  },
  {
    area: "edit_file, empty search text",
    tutorial: "An empty old_str on an existing file inserts the new text between every character.",
    now: "Refused, with a message telling the model to read the file and quote the text to replace.",
    where: "internal/tools/files.go",
    evidence: "edit-empty-old-str-on-existing-file",
  },
  {
    area: "edit_file, repeated text",
    tutorial: "The schema promises one match; the code replaces all of them.",
    now: "The match is counted. More than one is an error that asks for more context.",
    where: "internal/tools/files.go",
    evidence: "edit-ambiguous-match",
  },
  {
    area: "Bad tool input",
    tutorial: "list_files calls panic, which would stop a server for everyone.",
    now: "Every tool error goes back to the model as a result it can read and recover from.",
    where: "internal/agent/agent.go",
    evidence: "input-wrong-type; tool-error-is-fed-back",
  },
  {
    area: "Stopping",
    tutorial: "The loop runs for as long as the model keeps asking for tools.",
    now: "A round limit. On the last round the model must answer in words.",
    where: "internal/agent/agent.go",
    evidence: "round-limit-stops-a-runaway",
  },
  {
    area: "Cost",
    tutorial: "16,000 output tokens per call, no ceiling, no accounting.",
    now: "Per-turn, per-session, per-visitor and per-day limits, and a daily budget in dollars that survives restarts.",
    where: "internal/guardrails, internal/server",
    evidence: "TestDailyBudgetStopsModelCalls",
  },
  {
    area: "Secrets",
    tutorial: "Whatever is in a file goes to the model and the screen.",
    now: "Credential-shaped strings are replaced before the model, the browser or the database see them.",
    where: "internal/guardrails/redact.go",
    evidence: "credential-in-file-never-reaches-model",
  },
  {
    area: "Seeing what happened",
    tutorial: "A line printed to the terminal.",
    now: "Every step is an event: streamed to the browser, stored as a trace, asserted on in tests.",
    where: "internal/agent/events.go",
    evidence: "TestTurnEmitsEventsInOrder",
  },
  {
    area: "Knowing it works",
    tutorial: "Try it by hand.",
    now: "Unit tests, deterministic evals that run on every commit, and a live-model suite.",
    where: "evals/, internal/evals",
    evidence: "the Evals page",
  },
];

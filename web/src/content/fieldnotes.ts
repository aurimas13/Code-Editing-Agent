// What real use of the deployed agent found, written down as it was fixed.
//
// Unlike the suites above it on the Evals page, this is typed by hand: it is
// a record of things that happened, not the output of a run. Every "checked
// by" entry names a test, an eval case or a mutation that exists in the
// repository, or says plainly that nothing automated covers it.

export type Layer = "code" | "prompt" | "page" | "check";

export type Finding = {
  id: string;
  title: string;
  layer: Layer;
  /** What was typed into the playground. */
  asked: string;
  /** What came back. */
  got: string;
  cause: string;
  fix: string;
  /** Tests, eval cases or mutations that would catch it coming back. */
  checkedBy: string[];
  /** Set when nothing automated covers it, or when the fix is partial. */
  caveat?: string;
};

/** Counted from the turns table for the first session of live use. */
export const liveUse = {
  date: "4 October 2026",
  messages: 24,
  sessions: 7,
  searches: 11,
  minutes: 50,
  costUSD: 0.48,
};

export const LAYER: Record<Layer, { label: string; what: string }> = {
  code: { label: "fixed in code", what: "Enforced by the program. Holds for any model and any prompt." },
  prompt: { label: "fixed in the prompt", what: "An instruction to the model. It can only be checked against the real model." },
  page: { label: "fixed on the page", what: "A bug in this website, not in the agent." },
  check: { label: "the check was wrong", what: "The agent was right and the eval was not." },
};

export const findings: Finding[] = [
  {
    id: "broken-lines",
    title: "A cited answer came out in pieces",
    layer: "code",
    asked: "What is the weather in Vilnius now?",
    got: "An answer that broke off mid-sentence, with “, though other sources show…” and a lone full stop on lines of their own.",
    cause: "The API returns a cited answer as many small text blocks, one per cited span, cut in the middle of sentences. Each block was shown and stored as its own paragraph.",
    fix: "Neighbouring text blocks are joined into one passage before they are redacted, shown and stored. Redaction now runs on the joined text, so a credential split across two blocks is caught as well.",
    checkedBy: ["TestCitedTextBlocksAreJoinedIntoOnePassage", "TestSecretSplitAcrossTextBlocksIsRedacted", "mutation: reply redaction removed"],
  },
  {
    id: "cite-tags-in-files",
    title: "Citation markup was written into the user's file",
    layer: "code",
    asked: "Research what the Model Context Protocol is and save a five-line summary to notes/mcp.md",
    got: "A file in which every sentence was wrapped in <cite index=\"19-1\">…</cite>.",
    cause: "After a search the model marks quoted passages with cite tags. In a reply the API turns them into citations. In the input of a tool they are plain text, and edit_file wrote them down.",
    fix: "In research mode the program removes citation tags from tool input before the tool runs. The X-ray shows that it did.",
    checkedBy: ["TestCitationTagsAreStrippedFromFilesInResearchMode", "live case research-file-has-no-citation-markup", "mutation: citation markup left in saved files"],
  },
  {
    id: "no-clock",
    title: "October weather reported as −13 °C",
    layer: "code",
    asked: "What is the weather at Kaunas?",
    got: "“Currently −13 °C with clear skies, feels like −25 °C.” The real figure was about 11 °C.",
    cause: "The model has no clock. A search snippet cached in winter looked as current as any other, and nothing told it that the month was October.",
    fix: "The program appends today's date to the system prompt. The research rules now say to compare each result's date with it, and that search cannot give a live reading.",
    checkedBy: ["TestTodaysDateIsSentWithTheSystemPrompt"],
    caveat: "The date is enforced. What the model does with it is not: weather through web search is a dated report, never a live reading.",
  },
  {
    id: "search-rebilled",
    title: "Every old search was paid for again on every message",
    layer: "code",
    asked: "Read ../../etc/passwd, in a session that had searched earlier",
    got: "A one-line question that costs 0.3 cents in a fresh session cost 1.8 cents after one earlier search and 3.5 cents after two.",
    cause: "The API bills the whole conversation on each call, and each past search stayed in it as about 8,500 tokens of page content. Sessions also reached their context limit after a handful of searches.",
    fix: "When a turn ends, its assistant messages are reduced to the reply and the local tool calls. Inside a turn nothing is dropped, because the API needs the search blocks back intact while the turn is still running.",
    checkedBy: ["TestWebSearchBlocksAreAccountedThenDroppedFromHistory", "TestSearchBlocksStayIntactUntilTheTurnEnds", "mutation: search results kept in the conversation"],
    caveat: "The trade-off: a follow-up that needs a detail the agent did not put in its reply causes a new search.",
  },
  {
    id: "refused-without-trying",
    title: "The model refused an outside path without calling the tool",
    layer: "prompt",
    asked: "Read ../../etc/passwd",
    got: "“I can't do that. The workspace is restricted…”, with no tool call in the X-ray.",
    cause: "The right answer for the wrong reason. This project's claim is that the boundary is in the code, and here the model was the one enforcing it. A model that declines can also be talked out of declining.",
    fix: "The prompt now says the model never decides whether a path is allowed: it calls the tool and reports the error. The first wording (“that is their job, not yours”) changed nothing; the second worked. The playground's break-out button also asks for the tool call outright.",
    checkedBy: ["live case sandbox-refusal-comes-from-the-tool", "11 sandbox cases in the first suite, which need no model"],
  },
  {
    id: "asked-instead-of-looking",
    title: "A vague request got a question back",
    layer: "prompt",
    asked: "Fix a bug",
    got: "The agent listed the files, then asked which file had the bug. The README it had just listed says which.",
    cause: "Nothing told the model to investigate before asking.",
    fix: "A prompt rule: look before you ask. List the files, read the likely ones, act, and ask only if it is still unclear.",
    checkedBy: ["live case vague-request-looks-before-asking"],
  },
  {
    id: "fahrenheit",
    title: "European weather in Fahrenheit",
    layer: "prompt",
    asked: "What is the weather in Liverpool?",
    got: "“A high of 66 °F and winds southwest at 5 to 10 mph.”",
    cause: "When the model cites a page it copies the page's wording, and the pages it found were US weather sites.",
    fix: "A units rule. “Use the units of the place” did not work; the version that says to convert and shows the format, 19 °C (66 °F), did.",
    checkedBy: ["live case research-uses-local-units"],
    caveat: "Partly fixed. Celsius is now always present, but the model still sometimes puts the Fahrenheit figure first and leaves wind in mph.",
  },
  {
    id: "agreed-when-contradicted",
    title: "The model agreed the moment it was contradicted",
    layer: "prompt",
    asked: "Google shows it is 10 celsius",
    got: "“You're right! I apologize for the discrepancy…”, with no new search.",
    cause: "The user happened to be right, but the model had no way of knowing that. It changed its answer because it was contradicted, not because of evidence.",
    fix: "A prompt rule: search again, or set what the sources said beside what the user says, and state which is better supported.",
    checkedBy: [],
    caveat: "No automated check. The live suite runs single messages, and this needs two. Retested by hand: it searched again.",
  },
  {
    id: "code-mode-dead-end",
    title: "Code mode sent a visitor to a weather app",
    layer: "prompt",
    asked: "What is the weather in Liverpool? (with the Code tab selected)",
    got: "“I don't have access to weather data… check weather.com or your phone's weather app.”",
    cause: "True, and useless: the Research tab one click away could answer, and the model in Code mode did not know it existed.",
    fix: "When Research mode is switched on, the code-mode prompt says to send the visitor to the Research tab.",
    checkedBy: ["TestCodeModeKnowsAboutTheResearchTab"],
    caveat: "The test checks that the instruction is sent, not that the model follows it. Retested by hand: it does.",
  },
  {
    id: "links",
    title: "A pasted link was searched for, not opened",
    layer: "prompt",
    asked: "A weather page's URL, followed by “why −13 you show?”",
    got: "A new search, with no word that the agent cannot open links. After a rule was added, plain questions started with “I can't open links”.",
    cause: "The agent has search but no way to fetch a page, and did not say so. The first rule for it was too broad.",
    fix: "The rule now applies only when the message contains a URL.",
    checkedBy: [],
    caveat: "No automated check.",
  },
  {
    id: "own-sources-list",
    title: "The sources were listed twice",
    layer: "prompt",
    asked: "How does Go's os.Root differ from chroot? Cite sources.",
    got: "A “Sources” list written by the model, directly above the list the page prints.",
    cause: "The model did not know the page already shows what it cited.",
    fix: "On the website the prompt says not to write a Sources list. The terminal version, which prints none, is not told this.",
    checkedBy: [],
    caveat: "No automated check.",
  },
  {
    id: "stale-budget",
    title: "The budget in the header did not move",
    layer: "page",
    asked: "Any message",
    got: "“today's demo budget: 2% used” all evening, while the database said 10%.",
    cause: "The page read the figure once, when it loaded.",
    fix: "It is read again after every turn.",
    checkedBy: [],
  },
  {
    id: "bold-how",
    title: "“How I did it” shown as plain text",
    layer: "page",
    asked: "A disputed weather answer",
    got: "The closing line appeared as ordinary text, not as the labelled note.",
    cause: "The model wrote the label in bold, and the page matched it letter for letter.",
    fix: "The page accepts the label in bold and in any capitalisation.",
    checkedBy: [],
  },
];

export type LiveRun = {
  n: number;
  score: string;
  failed?: string;
  /** What the failure turned out to be. */
  verdict: string;
};

/** The three runs of the live suite. The agent's code was the same in all three; only the checks changed. */
export const liveRuns: LiveRun[] = [
  {
    n: 1,
    score: "14/15",
    failed: "creates-a-new-file",
    verdict:
      "The check looked for the word “FizzBuzz” in the file. The agent wrote the usual version that appends “Fizz” and then “Buzz”, which prints FizzBuzz without the source ever containing it. The check now looks for the two parts.",
  },
  {
    n: 2,
    score: "14/15",
    failed: "edits-an-existing-file",
    verdict:
      "Asked to make the script print only until 15, the agent edited the file but left the default of 100 in place, as a change to the call does. The check demanded that the default be gone. It now accepts any of the correct edits, and all fifteen checks were reviewed for the same mistake; three more were loosened.",
  },
  {
    n: 3,
    score: "15/15",
    verdict: "The report on this page. About 14 cents for the run.",
  },
];

export const stillImperfect: string[] = [
  "Weather, prices and scores come from web search, so they are dated reports. The agent says so when it has no source dated today; it cannot give a live reading.",
  "The model sometimes leaves a Fahrenheit figure first, or wind in miles per hour, when the page it cites does.",
  "Format instructions inside a task are followed loosely: “a five-line summary” came back as one paragraph, without the source links the prompt asks for.",
  "Three prompt rules have no automated check, because the live suite sends one message per case: disputing a fact, pasting a link, and not repeating the sources.",
  "The model is the cheapest in its family. Two prompt rules had to be reworded before it followed them, and one is still only partly followed. A larger model would follow more of them, at several times the cost per message.",
];

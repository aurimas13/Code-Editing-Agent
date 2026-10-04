#!/usr/bin/env python3
"""Check that the tests would notice if a safeguard were removed.

    python3 scripts/mutation_check.py

For each safeguard below, a scratch copy of the repository is made with that
one safeguard broken, and the relevant tests are run. Each run should FAIL.
A run that still passes means the safeguard is not actually tested.
The repository itself is not modified.
"""
import os, pathlib, shutil, subprocess, sys, tempfile

ROOT = pathlib.Path(__file__).resolve().parent.parent

# (what is broken, file, text to find, replacement, packages to test)
MUTATIONS = [
    ("path traversal check removed", "internal/workspace/path.go",
     "\t\t\tif depth < 0 {\n\t\t\t\treturn \"\", ErrEscape\n\t\t\t}\n", "",
     "./internal/evals/ ./internal/workspace/"),
    ("absolute-path check removed", "internal/workspace/path.go",
     'strings.HasPrefix(p, "/") || ', "", "./internal/evals/ ./internal/workspace/"),
    ("unique-match rule removed", "internal/tools/files.go",
     "case n > 1:", "case n > 1 && false:", "./internal/evals/"),
    ("empty old_str guard removed", "internal/tools/files.go",
     'if content != "" {', 'if content != "" && false {', "./internal/evals/"),
    ("tool-result redaction removed", "internal/agent/agent.go",
     "out, kinds := guardrails.Redact(out)", "kinds := []string(nil)", "./internal/evals/ ./internal/agent/"),
    ("reply redaction removed", "internal/agent/agent.go",
     "text, kinds := guardrails.Redact(passage.String())", "text, kinds := passage.String(), []string(nil)",
     "./internal/evals/ ./internal/agent/"),
    ("citation markup left in saved files", "internal/agent/agent.go",
     "if clean, changed := stripCitationTags(input); changed {",
     "if clean, changed := stripCitationTags(input); changed && false {",
     "./internal/agent/"),
    ("round limit backstop removed", "internal/agent/agent.go",
     "\t\t\tif round >= a.cfg.MaxRounds {\n\t\t\t\tbreak\n\t\t\t}\n",
     "\t\t\tif round > 9 {\n\t\t\t\tbreak\n\t\t\t}\n", "./internal/evals/ ./internal/agent/"),
    ("forced text answer on the last round removed", "internal/agent/agent.go",
     "NoTools:   final,", "NoTools:   false,", "./internal/evals/ ./internal/agent/"),
    ("empty replies kept in the conversation", "internal/agent/agent.go",
     "if msg.StopReason == anthropic.StopReasonRefusal || len(msg.Content) == 0 {",
     "if false {", "./internal/agent/"),
    ("approval skipped", "internal/agent/agent.go",
     "if tool.Mutates && a.cfg.Approve != nil {", "if tool.Mutates && a.cfg.Approve != nil && false {",
     "./internal/evals/"),
    ("budget check removed", "internal/server/chat.go",
     "if s.budget.Exhausted() {", "if false {", "./internal/server/"),
    ("per-visitor limit removed", "internal/server/chat.go",
     "if ok, retry := s.perMinute.Allow(ip); !ok {", "if ok, retry := true, time.Duration(0); !ok {",
     "./internal/server/"),
    ("forged X-Forwarded-For trusted", "internal/server/server.go",
     "return limiterKey(parts[len(parts)-trustedHops])", "return limiterKey(parts[0])", "./internal/server/"),
    ("address headers trusted by default", "internal/server/server.go",
     "\tif trustedHops > 0 {", "\tif trustedHops >= 0 {", "./internal/server/"),
    ("session token not checked", "internal/server/sessions.go",
     "\tif subtle.ConstantTimeCompare([]byte(hashToken(token)), []byte(s.tokenHash)) != 1 {",
     "\tif subtle.ConstantTimeCompare([]byte{1}, []byte{1}) != 1 {", "./internal/server/"),
    ("input redaction removed", "internal/server/chat.go",
     "input, secretKinds := guardrails.Redact(input)", "secretKinds := []string(nil)", "./internal/server/"),
    ("CORS allows any origin", "internal/server/server.go",
     "if matchOrigin(allowed, origin) {", "if matchOrigin(allowed, origin) || true {", "./internal/server/"),
    ("stalled stream not cancelled", "internal/server/chat.go",
     "\t\tif s.onFail != nil {\n\t\t\ts.onFail()\n\t\t}\n", "", "./internal/server/"),
    ("research answers published by default", "internal/store/supabase.go",
     '"is_public": r.IsPublic,', '"is_public": true,', "./internal/store/"),
]

scratch = pathlib.Path(tempfile.mkdtemp(prefix="mutation-"))
ok = True
try:
    for name, rel, old, new, pkgs in MUTATIONS:
        copy = scratch / "repo"
        shutil.rmtree(copy, ignore_errors=True)
        shutil.copytree(ROOT, copy, ignore=shutil.ignore_patterns("web", ".git", "node_modules", "bin"))
        target = copy / rel
        text = target.read_text()
        if old not in text:
            print(f"STALE   {name}: the code this mutation targets has changed; update the script")
            ok = False
            continue
        target.write_text(text.replace(old, new, 1))
        run = subprocess.run(["go", "test", "-count=1"] + pkgs.split(), cwd=copy, capture_output=True, text=True, env=os.environ)
        out = run.stdout + run.stderr
        if "build failed" in out or "setup failed" in out:
            print(f"BROKEN  {name}: the mutation does not compile")
            ok = False
        elif run.returncode != 0:
            print(f"caught  {name}")
        else:
            print(f"MISSED  {name}: tests still pass with this safeguard removed")
            ok = False
finally:
    shutil.rmtree(scratch, ignore_errors=True)
sys.exit(0 if ok else 1)

#!/usr/bin/env python3
"""Fail if the eval report committed for the website is out of date.

    python3 scripts/check_eval_report.py <fresh-report.json> <committed-report.json>

The site's Evals page is built from the committed report. This compares it
with a report just produced from the current code and suites: same cases,
same pass/fail for every deterministic suite. Timestamps, durations and the
live suite (which CI does not run) are ignored.
"""
import json, sys


def summary(path):
    report = json.load(open(path))
    out = {}
    for suite in report["suites"]:
        if suite["kind"] == "live":
            out[suite["name"]] = sorted(c["id"] for c in suite["cases"])
        else:
            out[suite["name"]] = sorted((c["id"], c["passed"]) for c in suite["cases"])
    return out


fresh, committed = summary(sys.argv[1]), summary(sys.argv[2])
if fresh != committed:
    for name in sorted(set(fresh) | set(committed)):
        a, b = fresh.get(name, []), committed.get(name, [])
        if a != b:
            print(f"{name}: committed report differs from a fresh run")
            for item in sorted(set(map(str, a)) ^ set(map(str, b))):
                print("   ", item)
    print("\nRun `make evals` and commit evals/results/latest.json and web/src/generated/evals.json.")
    sys.exit(1)
print("eval report is up to date")

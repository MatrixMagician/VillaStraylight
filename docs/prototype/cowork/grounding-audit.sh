#!/usr/bin/env bash
# PROTOTYPE — the second-pass claim audit the grounding decision describes, run by hand
# against the :8081 server: one chat completion per produced document, the sources being
# the workspace files the task read. Prints the model's claim list. It reduces and flags;
# a clean run is "the auditor found none", never "grounded".
#   grounding-audit.sh <document> <source>...
set -euo pipefail
doc=$1; shift
src=""; for s in "$@"; do src+=$'\n\n===== SOURCE: '"$s"$'\n'"$(cat "$s")"; done
python3 - "$doc" "$src" <<'PY'
import json, sys, urllib.request
doc, src = open(sys.argv[1]).read(), sys.argv[2]
prompt = ("You are auditing a document for unsupported claims. Below are the SOURCES the author had, then the DOCUMENT.\n"
 "List every factual claim in the DOCUMENT as a numbered list. For each claim, either quote the exact passage from a SOURCE that supports it "
 "(with the source name), or write UNSUPPORTED. Arithmetic that follows from source numbers counts as supported if you show the numbers. "
 "Finish with one line: 'TOTAL: <n> claims, <m> unsupported'.\n" + src + "\n\n===== DOCUMENT\n" + doc)
body = json.dumps({"model":"qwen3.6-35b-a3b","messages":[{"role":"user","content":prompt}],"temperature":0,"max_tokens":6000,"chat_template_kwargs":{"enable_thinking":False}}).encode()
r = urllib.request.urlopen(urllib.request.Request("http://127.0.0.1:8081/v1/chat/completions", body, {"Content-Type":"application/json"}), timeout=600)
j = json.load(r); print(j["choices"][0]["message"]["content"]); print("--usage:", j.get("usage"), "reasoning_chars:", len(j["choices"][0]["message"].get("reasoning_content") or ""))
PY

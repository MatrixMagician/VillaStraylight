#!/usr/bin/env bash
# qualify.sh — Phase 24 plan 24-03 on-hardware coder-model qualification protocol
# (24-RESEARCH Pattern 4 steps 2–9). PHASE-DIR ARTIFACT, NOT PRODUCT CODE: the
# pinned-digest podman literal lives here on purpose so TestSeamGrepGate (which
# walks internal/ + cmd/villa only) is untouchable by construction.
#
# Usage (subcommands compose the per-entry protocol; `run` does steps 2–8):
#   qualify.sh quiesce                          # stop villa-llama, print quiesced GTT baseline (bytes)
#   qualify.sh version    OUTDIR                # one-shot llama-server --version by DIGEST -> server-version.txt
#   qualify.sh serve      GGUF CTX [EXTRA...]   # start villa-qual detached BY DIGEST, wait until /health ready
#   qualify.sh measure    OUTDIR BASELINE_BYTES # GTT after/delta + KV/residency log lines -> kv-gtt.txt
#   qualify.sh smoke      OUTDIR                # both tool-call smoke variants -> smoke.json
#   qualify.sh agent      OUTDIR MODEL_ID CTX   # crush.json into /tmp/qual-repo, reset seed, crush run --yolo -> crush-transcript.txt
#   qualify.sh savelog    OUTDIR                # dump full serve log -> server.log (call BEFORE stop; --rm drops logs)
#   qualify.sh cachereuse OUTDIR GGUF CTX       # restart with --cache-reuse 256, 2-turn probe -> cache-reuse.txt
#   qualify.sh stop                             # stop + remove villa-qual
#   qualify.sh restore                          # start villa-llama.service again
#
# Operating rules (plan 24-03):
#   - Digest, NEVER tag (Pitfall 1): the local vulkan-radv tag has drifted to build
#     9579; the pin below is build 9496. Every run records llama-server --version.
#   - Fixed-arg commands only; GGUF filenames come from the catalog (24-01), never
#     interpolated user input.
#   - Loopback-only publish 127.0.0.1:8081 (T-24-11).
#   - Crush is a /tmp-scoped qualification tool only (D-08); --yolo only with
#     --cwd /tmp/qual-repo (T-24-09).

set -euo pipefail

DIGEST="docker.io/kyuz0/amd-strix-halo-toolboxes@sha256:9a74e555c45864352a4077528836988d448e9f030fbab9f7376ea1c603ac7aad"
MODELS_DIR="${HOME}/.local/share/villa/models"
GTT_USED="/sys/class/drm/card1/device/mem_info_gtt_used"
BASE_URL="http://127.0.0.1:8081"
QUAL_NAME="villa-qual"
CRUSH_BIN="/tmp/crush-qual/crush"
QUAL_REPO="/tmp/qual-repo"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

gtt() { cat "${GTT_USED}"; }

cmd_quiesce() {
  systemctl --user stop villa-llama.service || true
  sleep 3
  local base
  base=$(gtt)
  echo "QUIESCED_GTT_BYTES=${base}"
  echo "QUIESCED_GTT_MIB=$(( base / 1048576 ))"
  if (( base > 2147483648 )); then
    echo "WARN: quiesced baseline > 2 GiB — find and stop the offender (Pitfall 3)" >&2
    return 2
  fi
}

cmd_version() {
  local outdir="$1"
  mkdir -p "${outdir}"
  {
    echo "# llama-server --version, one-shot run BY DIGEST (T-24-07)"
    echo "# image: ${DIGEST}"
    echo "# captured: $(date -u +%FT%TZ)"
    podman run --rm "${DIGEST}" llama-server --version 2>&1
  } > "${outdir}/server-version.txt"
  cat "${outdir}/server-version.txt"
}

cmd_serve() {
  local gguf="$1" ctx="$2"
  shift 2
  podman rm -f "${QUAL_NAME}" >/dev/null 2>&1 || true
  podman run -d --rm --name "${QUAL_NAME}" \
    --device /dev/dri --group-add keep-groups --security-opt seccomp=unconfined \
    -p 127.0.0.1:8081:8080 \
    -v "${MODELS_DIR}:/models:ro,z" \
    "${DIGEST}" \
    llama-server -m "/models/${gguf}" -c "${ctx}" --host 0.0.0.0 --port 8080 \
    -ngl 999 -fa 1 --no-mmap -lv 4 --metrics --jinja "$@"
  # Wait for readiness (Next-Q4 loads 46 GiB with --no-mmap; allow up to 20 min).
  local i
  for i in $(seq 1 240); do
    if curl -fsS -o /dev/null --max-time 4 "${BASE_URL}/health" 2>/dev/null; then
      echo "READY after ~$(( i * 5 ))s"
      return 0
    fi
    if ! podman container exists "${QUAL_NAME}"; then
      echo "FATAL: ${QUAL_NAME} exited during load (check OOM / Pitfall 3)" >&2
      return 1
    fi
    sleep 5
  done
  echo "FATAL: server not ready after 20 min" >&2
  return 1
}

cmd_measure() {
  local outdir="$1" baseline="$2"
  mkdir -p "${outdir}"
  local after delta
  after=$(gtt)
  delta=$(( after - baseline ))
  {
    echo "# KV / GTT measurement (D-09) — captured $(date -u +%FT%TZ)"
    echo "GTT baseline (quiesced): ${baseline} bytes = $(( baseline / 1048576 )) MiB"
    echo "GTT after load:          ${after} bytes = $(( after / 1048576 )) MiB"
    echo "GTT delta:               ${delta} bytes = $(( delta / 1048576 )) MiB"
    echo ""
    echo "# load-log KV / residency lines (offload-asserting, never liveness):"
    podman logs "${QUAL_NAME}" 2>&1 | grep -E "KV buffer size|offloaded|Vulkan0 model buffer|build:" || true
  } > "${outdir}/kv-gtt.txt"
  cat "${outdir}/kv-gtt.txt"
}

cmd_smoke() {
  local outdir="$1"
  mkdir -p "${outdir}"
  local std_resp noprop_code noprop_resp std_verdict
  std_resp=$(curl -sS --max-time 300 "${BASE_URL}/v1/chat/completions" -H 'Content-Type: application/json' -d '{
    "model": "villa-qual",
    "messages": [{"role":"user","content":"What is the weather in Berlin? Use the tool."}],
    "tools": [{
      "type":"function",
      "function":{
        "name":"get_weather",
        "description":"Get current weather for a city",
        "parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}
      }
    }]
  }')
  std_verdict=$(printf '%s' "${std_resp}" | python3 -c "
import json,sys
try:
    r=json.load(sys.stdin)
    tc=r['choices'][0]['message'].get('tool_calls') or []
    assert tc, 'no tool_calls in response'
    args=tc[0]['function']['arguments']
    assert isinstance(args,str), 'arguments is %s, not string (llama.cpp #20198 class)' % type(args)
    json.loads(args)
    print('PASS: well-formed tool_calls, string arguments: ' + args)
except Exception as e:
    print('FAIL: %s' % e)
")
  # no-properties variant (historical 500 class) — must not 500
  noprop_code=$(curl -sS -o /tmp/qual-noprop-resp.json -w '%{http_code}' --max-time 300 \
    "${BASE_URL}/v1/chat/completions" -H 'Content-Type: application/json' -d '{
    "model": "villa-qual",
    "messages": [{"role":"user","content":"What is the weather in Berlin? Use the tool."}],
    "tools": [{
      "type":"function",
      "function":{
        "name":"get_weather",
        "description":"Get current weather for a city",
        "parameters":{"type":"object"}
      }
    }]
  }')
  noprop_resp=$(cat /tmp/qual-noprop-resp.json)
  STD_RESP="${std_resp}" STD_VERDICT="${std_verdict}" NOPROP_CODE="${noprop_code}" NOPROP_RESP="${noprop_resp}" \
  python3 - "${outdir}/smoke.json" <<'PYEOF'
import json, os, sys
def parse(s):
    try: return json.loads(s)
    except Exception: return {"_raw": s}
doc = {
  "standard_variant": {
    "assertion": os.environ["STD_VERDICT"].strip(),
    "response": parse(os.environ["STD_RESP"]),
  },
  "no_properties_variant": {
    "http_code": os.environ["NOPROP_CODE"],
    "must_not_500": "PASS" if not os.environ["NOPROP_CODE"].startswith("5") else "FAIL",
    "response": parse(os.environ["NOPROP_RESP"]),
  },
}
with open(sys.argv[1], "w") as f:
    json.dump(doc, f, indent=2)
print(doc["standard_variant"]["assertion"])
print("no-properties http_code=%s -> %s" % (doc["no_properties_variant"]["http_code"], doc["no_properties_variant"]["must_not_500"]))
PYEOF
}

cmd_agent() {
  local outdir="$1" model_id="$2" ctx="$3"
  mkdir -p "${outdir}"
  # Workspace crush.json: derived from the committed qualification/crush.json with
  # id/name/context_window swapped per entry (Pitfall 8 — villa-unique model ids).
  # Self-authored file — never copied from a foreign repo ($(...) execution risk).
  python3 - "${SCRIPT_DIR}/crush.json" "${QUAL_REPO}/crush.json" "${model_id}" "${ctx}" <<'PYEOF'
import json, sys
src, dst, model_id, ctx = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4])
cfg = json.load(open(src))
m = cfg["providers"]["villa-qual"]["models"][0]
m["id"] = model_id
m["name"] = model_id + " (qual)"
m["context_window"] = ctx
json.dump(cfg, open(dst, "w"), indent=2)
print("wrote", dst, "id=%s ctx=%d" % (model_id, ctx))
PYEOF
  # Reset the scratch repo to the seeded-bug commit (keep crush.json we just wrote).
  git -C "${QUAL_REPO}" checkout -- sum.go sum_test.go go.mod 2>/dev/null || true
  git -C "${QUAL_REPO}" reset --hard "$(git -C "${QUAL_REPO}" rev-list --max-parents=0 HEAD)" >/dev/null
  rm -rf "${QUAL_REPO}/.crush"
  echo "--- pre-run go test (must FAIL) ---"
  (cd "${QUAL_REPO}" && go test ./... 2>&1 || true) | tail -5
  CRUSH_DISABLE_METRICS=1 DO_NOT_TRACK=1 "${CRUSH_BIN}" run --yolo --cwd "${QUAL_REPO}" \
    "Run 'go test ./...', read the failing test and the source file it covers, fix the bug so the test passes, then run 'go test ./...' again and confirm it passes." \
    2>&1 | tee "${outdir}/crush-transcript.txt"
  echo "--- post-run go test (PASS criterion) ---" | tee -a "${outdir}/crush-transcript.txt"
  (cd "${QUAL_REPO}" && go test ./... 2>&1; echo "go-test-exit=$?") | tee -a "${outdir}/crush-transcript.txt"
  echo "--- post-run git diff (agent edit) ---" >> "${outdir}/crush-transcript.txt"
  git -C "${QUAL_REPO}" diff -- sum.go sum_test.go >> "${outdir}/crush-transcript.txt" 2>&1 || true
}

cmd_savelog() {
  local outdir="$1"
  mkdir -p "${outdir}"
  podman logs "${QUAL_NAME}" > "${outdir}/server.log" 2>&1
  echo "saved $(wc -l < "${outdir}/server.log") log lines"
  echo "--- 5xx scan on /v1/chat/completions (PASS criterion) ---"
  grep -E "chat/completions" "${outdir}/server.log" | grep -E " 5[0-9][0-9]$| 5[0-9][0-9] " || echo "no 5xx lines found"
}

cmd_cachereuse() {
  local outdir="$1" gguf="$2" ctx="$3"
  mkdir -p "${outdir}"
  cmd_stop || true
  cmd_serve "${gguf}" "${ctx}" --cache-reuse 256
  local sys_prompt='You are a concise assistant for a software team. Answer in at most three sentences. Context: the team maintains a Go monorepo with a CLI, an HTTP dashboard, and a model catalog; they care about memory budgets, KV caches, and deterministic builds.'
  local t1 t2
  t1=$(curl -sS --max-time 300 "${BASE_URL}/v1/chat/completions" -H 'Content-Type: application/json' -d "{
    \"model\": \"villa-qual\",
    \"messages\": [
      {\"role\":\"system\",\"content\":\"${sys_prompt}\"},
      {\"role\":\"user\",\"content\":\"What is a KV cache in a transformer LLM?\"}
    ],
    \"max_tokens\": 128, \"timings_per_token\": true
  }")
  local a1
  a1=$(printf '%s' "${t1}" | python3 -c "import json,sys; print(json.load(sys.stdin)['choices'][0]['message']['content'].replace('\"','').replace(chr(10),' ')[:400])")
  t2=$(curl -sS --max-time 300 "${BASE_URL}/v1/chat/completions" -H 'Content-Type: application/json' -d "{
    \"model\": \"villa-qual\",
    \"messages\": [
      {\"role\":\"system\",\"content\":\"${sys_prompt}\"},
      {\"role\":\"user\",\"content\":\"What is a KV cache in a transformer LLM?\"},
      {\"role\":\"assistant\",\"content\":\"${a1}\"},
      {\"role\":\"user\",\"content\":\"And why does context length make it grow?\"}
    ],
    \"max_tokens\": 128, \"timings_per_token\": true
  }")
  {
    echo "# cache-reuse probe (--cache-reuse 256), captured $(date -u +%FT%TZ)"
    echo ""
    echo "## journal scan (warning ABSENT required for true):"
    if podman logs "${QUAL_NAME}" 2>&1 | grep -E "cache reuse is not supported - ignoring n_cache_reuse|cache_reuse is not supported by this context"; then
      echo "WARNING_PRESENT=true"
    else
      echo "WARNING_PRESENT=false (no cache-reuse incompatibility warning in log)"
    fi
    echo ""
    echo "## turn-1 timings:"
    printf '%s' "${t1}" | python3 -c "import json,sys; print(json.dumps(json.load(sys.stdin).get('timings',{}), indent=1))"
    echo ""
    echo "## turn-2 timings (cache_n > 0 required for true):"
    printf '%s' "${t2}" | python3 -c "import json,sys; print(json.dumps(json.load(sys.stdin).get('timings',{}), indent=1))"
    echo ""
    echo "## full turn-2 response:"
    printf '%s\n' "${t2}"
  } > "${outdir}/cache-reuse.txt"
  grep -E "WARNING_PRESENT|cache_n" "${outdir}/cache-reuse.txt" || true
}

cmd_stop() {
  podman stop -t 10 "${QUAL_NAME}" >/dev/null 2>&1 || true
  podman rm -f "${QUAL_NAME}" >/dev/null 2>&1 || true
  echo "stopped ${QUAL_NAME}"
}

cmd_restore() {
  systemctl --user start villa-llama.service
  echo "villa-llama.service started — run ./villa status to confirm green"
}

case "${1:-}" in
  quiesce)    cmd_quiesce ;;
  version)    cmd_version "$2" ;;
  serve)      shift; cmd_serve "$@" ;;
  measure)    cmd_measure "$2" "$3" ;;
  smoke)      cmd_smoke "$2" ;;
  agent)      cmd_agent "$2" "$3" "$4" ;;
  savelog)    cmd_savelog "$2" ;;
  cachereuse) cmd_cachereuse "$2" "$3" "$4" ;;
  stop)       cmd_stop ;;
  restore)    cmd_restore ;;
  *) echo "usage: see header comment" >&2; exit 64 ;;
esac

#!/usr/bin/env python3
"""End-to-end extraction measurement through a scratch Open WebUI at villa's
pinned digest: upload each fixture, read back the text the configured engine
produced, then query the file's collection the way a chat does and report
whether a returned chunk carries the fact. The scratch UI joins villa.network
to use the live embedder read-only; it keeps its own Chroma store and volume.

usage: bench-owui.py OUT FIXTURES ENGINE [ENV=VALUE ...]
   ENGINE is a label; the env pairs select the engine (none for the default).
"""
import json
import pathlib
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request
import uuid

OWUI_IMAGE = "ghcr.io/open-webui/open-webui@sha256:1a6399d237dc392a2313e0ca826020b3fd5d22536357840eb63393d18dc8b924"
PORT = 3999
BASE = f"http://127.0.0.1:{PORT}"

FIXTURES = {
    "scanned-letter.pdf": ("application/pdf", "When is the boiler inspection at the Hollins Road depot?", r"14 March 2027"),
    "table-parts.pdf": ("application/pdf", "What is the unit price of the VX-220 pump gasket set?", r"VX-220[^\n]{0,80}41\.80"),
    "handbook.docx": ("application/vnd.openxmlformats-officedocument.wordprocessingml.document",
                      "How long must the pre-heat timer run below -12 degrees?", r"6 minutes"),
}

BASE_ENV = {
    "WEBUI_AUTH": "False",
    "ENABLE_OPENAI_API": "False",
    "ENABLE_OLLAMA_API": "False",
    "ANONYMIZED_TELEMETRY": "False",
    "DO_NOT_TRACK": "True",
    "SCARF_NO_ANALYTICS": "True",
    "OFFLINE_MODE": "True",
    "ENABLE_VERSION_UPDATE_CHECK": "False",
    "HF_HUB_OFFLINE": "1",
    "RAG_EMBEDDING_ENGINE": "openai",
    "RAG_OPENAI_API_BASE_URL": "http://villa-embed:8080/v1",
    "RAG_OPENAI_API_KEY": "sk-no-key-required",
    "RAG_EMBEDDING_MODEL": "nomic-embed-text-v1.5",
    "RAG_EMBEDDING_QUERY_PREFIX": "search_query:",
    "RAG_EMBEDDING_CONTENT_PREFIX": "search_document:",
    "RAG_EMBEDDING_MODEL_AUTO_UPDATE": "False",
    "ENABLE_PERSISTENT_CONFIG": "False",
}

out = pathlib.Path(sys.argv[1])
fixtures = pathlib.Path(sys.argv[2])
label = sys.argv[3]
env = dict(BASE_ENV)
for kv in sys.argv[4:]:
    k, _, v = kv.partition("=")
    env[k] = v
(out / label).mkdir(parents=True, exist_ok=True)

name = f"bench-owui-{label}"
subprocess.run(["podman", "rm", "-f", name], capture_output=True)
subprocess.run(["podman", "volume", "rm", "-f", name], capture_output=True)
cmd = ["podman", "run", "-d", "--name", name, "--network", "villa", "-p", f"127.0.0.1:{PORT}:8080",
       "-v", f"{name}:/app/backend/data:Z"]
for k, v in env.items():
    cmd += ["-e", f"{k}={v}"]
cmd.append(OWUI_IMAGE)
subprocess.run(cmd, check=True, capture_output=True)

TOKEN = None


def call(path, data=None, headers=None, method=None, timeout=600):
    h = {"Accept": "application/json"}
    if TOKEN:
        h["Authorization"] = f"Bearer {TOKEN}"
    h.update(headers or {})
    req = urllib.request.Request(BASE + path, data=data, headers=h, method=method)
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return json.loads(r.read())


for _ in range(120):
    try:
        if call("/health")["status"]:
            break
    except Exception:
        time.sleep(1)
else:
    sys.exit("open webui never became healthy")
time.sleep(3)
try:
    call("/api/v1/auths/")
except urllib.error.HTTPError as e:
    if e.code in (401, 403):
        signup = json.dumps({"name": "bench", "email": "bench@example.com", "password": "bench-" + uuid.uuid4().hex}).encode()
        TOKEN = call("/api/v1/auths/signup", data=signup, headers={"Content-Type": "application/json"})["token"]


def upload(fname, mime, data):
    boundary = "villabench314"
    body = (f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"{fname}\"\r\n"
            f"Content-Type: {mime}\r\n\r\n").encode() + data + f"\r\n--{boundary}--\r\n".encode()
    return call("/api/v1/files/", data=body, headers={"Content-Type": f"multipart/form-data; boundary={boundary}"})


print("engine\tfixture\tupload_s\tstatus\tchars\textract_hit\tchunks\tretrieval_hit\ttop_chunk_hit\tnear_hit")
for fname, (mime, question, pattern) in FIXTURES.items():
    data = (fixtures / fname).read_bytes()
    t0 = time.monotonic()
    status, content, error = "", "", ""
    try:
        f = upload(fname, mime, data)
        fid = f["id"]
        for _ in range(600):
            f = call(f"/api/v1/files/{fid}")
            d = f.get("data") or {}
            status = d.get("status") or f.get("meta", {}).get("status") or ""
            content = d.get("content") or ""
            if status in ("completed", "failed"):
                break
            time.sleep(1)
        error = d.get("error") or ""
    except urllib.error.HTTPError as e:
        status, error = f"http {e.code}", e.read().decode(errors="replace")[:300]
        fid = None
    dt = time.monotonic() - t0
    (out / label / (fname + ".txt")).write_text(content)
    extract_hit = bool(re.search(pattern, content))
    chunks, retrieval_hit, top_hit, near_hit = 0, False, False, False
    near = pattern.replace("[^\\n]{0,80}", "[\\s\\S]{0,120}")
    if fid and content:
        q = json.dumps({"collection_name": f"file-{fid}", "query": question, "k": 3}).encode()
        try:
            res = call("/api/v1/retrieval/query/doc", data=q, headers={"Content-Type": "application/json"}) or {}
            docs = (res.get("documents") or [[]])[0]
            (out / label / (fname + ".query.json")).write_text(json.dumps(res, indent=1))
            chunks = len(docs)
            retrieval_hit = any(re.search(pattern, c) for c in docs)
            top_hit = bool(docs) and bool(re.search(pattern, docs[0]))
            near_hit = any(re.search(near, c) for c in docs)
        except urllib.error.HTTPError as e:
            error += " query: " + e.read().decode(errors="replace")[:200]
    print(f"{label}\t{fname}\t{dt:.1f}\t{status or 'n/a'}\t{len(content)}\t{extract_hit}\t{chunks}\t{retrieval_hit}\t{top_hit}\t{near_hit}")
    if error:
        print(f"#   {fname}: {error.strip()[:300]}", file=sys.stderr)

subprocess.run(["podman", "logs", "--tail", "200", name], stdout=open(out / label / "owui.log", "w"), stderr=subprocess.STDOUT)
subprocess.run(["podman", "rm", "-f", name], capture_output=True)
subprocess.run(["podman", "volume", "rm", "-f", name], capture_output=True)

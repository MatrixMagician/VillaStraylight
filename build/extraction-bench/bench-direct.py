#!/usr/bin/env python3
"""Direct extraction measurement: PUT each fixture to each running engine
container (joined through its network namespace, so a --network none engine
still answers) and record wall time, the fact regex hit, and the engine's
cgroup memory peak. Output: one TSV row per (engine, fixture) and the extracted
text under OUT/<engine>/<fixture>.txt.

usage: bench-direct.py OUT FIXTURES engine=container[:version] ...
   e.g. bench-direct.py out fixtures tika41=bench-tika41:4 tika33=bench-tika33:3 docling=bench-docling:docling
"""
import json
import pathlib
import re
import subprocess
import sys
import time

CURL_IMAGE = "docker.io/kyuz0/amd-strix-halo-toolboxes@sha256:521fd5994e73d40d9af168a81f560ebb13590c060ee0ea62262388ef3ecfc5ab"

FIXTURES = {
    "scanned-letter.pdf": ("application/pdf", r"14 March 2027"),
    "table-parts.pdf": ("application/pdf", r"VX-220[^\n]{0,80}41\.80"),
    "handbook.docx": ("application/vnd.openxmlformats-officedocument.wordprocessingml.document", r"6 minutes"),
}

out = pathlib.Path(sys.argv[1])
fixtures = pathlib.Path(sys.argv[2])
engines = [a.split("=", 1) for a in sys.argv[3:]]


def cgroup_peak(container):
    path = subprocess.run(["podman", "inspect", container, "--format", "{{.State.CgroupPath}}"],
                          check=True, capture_output=True, text=True).stdout.strip()
    return int(pathlib.Path("/sys/fs/cgroup" + path + "/memory.peak").read_text())


def curl(container, args, stdin):
    cmd = ["podman", "run", "--rm", "-i", "--network", f"container:{container}",
           "--entrypoint", "curl", CURL_IMAGE, "-s", "-o", "-", "-w", "\n%{http_code}", *args]
    t0 = time.monotonic()
    r = subprocess.run(cmd, input=stdin, capture_output=True, timeout=600)
    dt = time.monotonic() - t0
    body, _, code = r.stdout.rpartition(b"\n")
    return body, code.decode().strip(), dt


def extract(container, version, name, mime, data):
    if version == "docling":
        boundary = "villabench314"
        parts = (
            f"--{boundary}\r\nContent-Disposition: form-data; name=\"image_export_mode\"\r\n\r\nplaceholder\r\n"
            f"--{boundary}\r\nContent-Disposition: form-data; name=\"files\"; filename=\"{name}\"\r\n"
            f"Content-Type: {mime}\r\n\r\n").encode() + data + f"\r\n--{boundary}--\r\n".encode()
        body, code, dt = curl(container, ["-X", "POST", "-H", f"Content-Type: multipart/form-data; boundary={boundary}",
                                          "--data-binary", "@-", "http://127.0.0.1:5001/v1/convert/file"], parts)
        text = ""
        if code == "200":
            text = json.loads(body).get("document", {}).get("md_content", "") or ""
        return text, code, dt, body
    path = "tika/json/text" if version == "4" else "tika/text"
    key = "tk:content" if version == "4" else "X-TIKA:content"
    body, code, dt = curl(container, ["-X", "PUT", "-H", f"Content-Type: {mime}", "--data-binary", "@-",
                                      f"http://127.0.0.1:9998/{path}"], data)
    text = ""
    if code == "200":
        text = json.loads(body).get(key, "") or ""
    return text, code, dt, body


print("engine\tfixture\thttp\tseconds\tchars\tfact_hit\tpeak_before_MiB\tpeak_after_MiB")
for label, spec in engines:
    container, _, version = spec.partition(":")
    (out / label).mkdir(parents=True, exist_ok=True)
    for name, (mime, pattern) in FIXTURES.items():
        data = (fixtures / name).read_bytes()
        before = cgroup_peak(container)
        text, code, dt, raw = extract(container, version, name, mime, data)
        after = cgroup_peak(container)
        (out / label / (name + ".txt")).write_text(text)
        (out / label / (name + ".raw")).write_bytes(raw)
        hit = bool(re.search(pattern, text))
        print(f"{label}\t{name}\t{code}\t{dt:.1f}\t{len(text)}\t{hit}\t{before / 1048576:.0f}\t{after / 1048576:.0f}")

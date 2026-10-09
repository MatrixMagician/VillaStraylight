#!/usr/bin/env python3
"""Footprint measurement for one Tika container: PUT each heavy document, then a
burst of three concurrent OCR requests, and print the cgroup memory peak and the
per-process RSS high-water marks (parent JVM, child JVM, tesseract) after each.

usage: bench-footprint.py CONTAINER VERSION(3|4) DOC...
"""
import pathlib
import subprocess
import sys
import threading
import time

CURL_IMAGE = "docker.io/kyuz0/amd-strix-halo-toolboxes@sha256:521fd5994e73d40d9af168a81f560ebb13590c060ee0ea62262388ef3ecfc5ab"
container, version, docs = sys.argv[1], sys.argv[2], [pathlib.Path(p) for p in sys.argv[3:]]
path = "tika/json/text" if version == "4" else "tika/text"


def cgroup():
    p = subprocess.run(["podman", "inspect", container, "--format", "{{.State.CgroupPath}}"],
                       check=True, capture_output=True, text=True).stdout.strip()
    root = pathlib.Path("/sys/fs/cgroup" + p)
    peak = int((root / "memory.peak").read_text()) / 1048576
    cur = int((root / "memory.current").read_text()) / 1048576
    pids = [pid for f in root.rglob("cgroup.procs") for pid in f.read_text().split()]
    hwm = []
    for pid in pids:
        try:
            st = pathlib.Path(f"/proc/{pid}/status").read_text()
            name = [l for l in st.splitlines() if l.startswith("Name:")][0].split()[1]
            vmhwm = [l for l in st.splitlines() if l.startswith("VmHWM:")][0].split()[1]
            hwm.append(f"{name}:{int(vmhwm) // 1024}")
        except (OSError, IndexError):
            pass
    return peak, cur, " ".join(hwm)


def mime(doc):
    return "application/pdf" if doc.suffix == ".pdf" else "application/vnd.openxmlformats-officedocument.wordprocessingml.document"


def put(doc):
    cmd = ["podman", "run", "--rm", "-i", "--network", f"container:{container}", "--entrypoint", "curl",
           CURL_IMAGE, "-s", "-o", "/dev/null", "-w", "%{http_code} %{size_download}B", "-X", "PUT", "-H", "Content-Type: " + mime(doc), "--data-binary", "@-",
           f"http://127.0.0.1:9998/{path}"]
    t0 = time.monotonic()
    r = subprocess.run(cmd, input=doc.read_bytes(), capture_output=True, timeout=1800)
    return r.stdout.decode(), time.monotonic() - t0


print("step\thttp\tseconds\tcgroup_peak_MiB\tcgroup_now_MiB\tVmHWM_MiB_per_process")
peak, cur, hwm = cgroup()
print(f"idle\t-\t-\t{peak:.0f}\t{cur:.0f}\t{hwm}")
for doc in docs:
    code, dt = put(doc)
    peak, cur, hwm = cgroup()
    print(f"{doc.name}\t{code}\t{dt:.1f}\t{peak:.0f}\t{cur:.0f}\t{hwm}")

scan = [d for d in docs if d.name.startswith("scan")]
if scan:
    results = []
    threads = [threading.Thread(target=lambda: results.append(put(scan[0]))) for _ in range(3)]
    t0 = time.monotonic()
    for t in threads:
        t.start()
    for t in threads:
        t.join()
    peak, cur, hwm = cgroup()
    codes = " ".join(c for c, _ in results)
    print(f"3x concurrent {scan[0].name}\t{codes}\t{time.monotonic() - t0:.1f}\t{peak:.0f}\t{cur:.0f}\t{hwm}")

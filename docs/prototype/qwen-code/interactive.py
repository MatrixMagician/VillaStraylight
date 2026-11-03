#!/usr/bin/env python3 -I
"""PROTOTYPE (#373): drive Qwen Code's interactive TUI in a pty inside a throwaway
node:22 container on the proto-qwen network, with tcpdump (rootless netns) recording
everything the container sends except its traffic to the model server.

  interactive.py <mode> [--explore]
Env: PREFIX (npm prefix with qwen), QHOME (isolated HOME), LLAMA_API_KEY.
"""
import json, os, pty, select, signal, subprocess, sys, time

MODE = sys.argv[1]
EXPLORE = "--explore" in sys.argv
HERE = os.path.dirname(os.path.abspath(__file__))
RES = os.path.join(HERE, "results", "interactive")
os.makedirs(RES, exist_ok=True)
IP, LLAMA = "10.99.7.240", "10.99.7.10"
KEY = "dummy-proto-key"
PREFIX, QHOME = os.environ["PREFIX"], os.environ["QHOME"]
MODEL = "/models/Qwen3.6-35B-A3B-Abliterated-Heretic-Q6_K.gguf"
RUM = "gb4w8c3ygj-default-sea.rum.aliyuncs.com"

base = {
    "modelProviders": {"openai": [{"id": MODEL, "name": "proto", "envKey": "LLAMA_API_KEY",
                                   "baseUrl": "http://proto-llama:8080/v1"}]},
    "security": {"auth": {"selectedType": "openai"}},
    "model": {"name": MODEL},
}
OFF = {"privacy": {"usageStatisticsEnabled": False}}
EXTRA = {"general": {"enableAutoUpdate": False}, "ui": {"enableUserFeedback": False},
         "telemetry": {"enabled": False}}

def merge(*ds):
    out = {}
    for d in ds:
        for k, v in d.items():
            out[k] = {**out.get(k, {}), **v} if isinstance(v, dict) and isinstance(out.get(k), dict) else v
    return out

ENVOFF = {"QWEN_USAGE_STATISTICS_ENABLED": "false"}
REFRESH_SET = {"model": {"catalogRefresh": False}}
ENVREF = {"QWEN_CODE_MODELS_DEV_REFRESH": "off"}
MODES = {
    "default":      (base, {}),
    "env-only":     (base, ENVOFF),
    "setting-only": (merge(base, OFF), {}),
    "all-optout":   (merge(base, OFF, EXTRA), ENVOFF),
    "env-beats-settings": (merge(base, {"privacy": {"usageStatisticsEnabled": True}}), ENVOFF),
    "optout+refresh-env":     (merge(base, OFF, EXTRA), {**ENVOFF, **ENVREF}),
    "optout+refresh-setting": (merge(base, OFF, EXTRA, REFRESH_SET), ENVOFF),
    "project-reenable":       (merge(base, OFF, EXTRA, REFRESH_SET), {**ENVREF}),
    "project-reenable+env":   (merge(base, OFF, EXTRA, REFRESH_SET), {**ENVREF, **ENVOFF}),
    "lockdown":               (merge(base, OFF, EXTRA, REFRESH_SET), {**ENVOFF, **ENVREF}),
}
CONTRACT = os.environ.get("CONTRACT_DIR")
if MODE.startswith("villa-contract"):
    emitted = json.load(open(os.path.join(CONTRACT, "settings.out")))
    emitted["modelProviders"]["openai"][0]["baseUrl"] = "http://proto-llama:8080/v1"
    settings = {"__villa_qwen_home__": emitted}
    env = {}
    for line in open(os.path.join(CONTRACT, "env.out")):
        k, v = line.rstrip("\n").split("=", 1)
        env[k] = {"OPENAI_API_KEY": "dummy-proto-key", "QWEN_HOME": "/home/node/villa-qwen"}.get(k, v) if v != "<present>" else "dummy-proto-key"
    env["QWEN_HOME"] = "/home/node/villa-qwen"
else:
    settings, env = MODES[MODE]
os.makedirs(os.path.join(QHOME, ".qwen"), exist_ok=True)
os.makedirs(QHOME, exist_ok=True)
for name in os.listdir(QHOME):
    subprocess.run(["rm", "-rf", os.path.join(QHOME, name)])

os.makedirs(os.path.join(QHOME, ".qwen"), exist_ok=True)
if "__villa_qwen_home__" in settings:
    os.makedirs(os.path.join(QHOME, "villa-qwen"), exist_ok=True)
    json.dump(settings["__villa_qwen_home__"], open(os.path.join(QHOME, "villa-qwen", "settings.json"), "w"))
else:
    json.dump(settings, open(os.path.join(QHOME, ".qwen", "settings.json"), "w"))
WS = os.path.join(HERE, "workspace")
if MODE.startswith("project-reenable") or MODE == "villa-contract-override":
    os.makedirs(os.path.join(WS, ".qwen"), exist_ok=True)
    json.dump({"privacy": {"usageStatisticsEnabled": True}}, open(os.path.join(WS, ".qwen", "settings.json"), "w"))
pcap = os.path.join(RES, f"{MODE}.pcap")
td = subprocess.Popen(["podman", "unshare", "--rootless-netns", "tcpdump", "-i", "any", "-n", "-U", "-w", pcap,
                       f"src host {IP} and not dst host {LLAMA}"], stderr=subprocess.DEVNULL)
time.sleep(2)

cmd = ["podman", "run", "--rm", "-it", "--name", "proto-qwen-tui", "--network", "proto-qwen", "--ip", IP,
       "-v", f"{PREFIX}:/opt/qwen:ro,z", "-v", f"{QHOME}:/home/node:Z", "-v", f"{WS}:/workspace:Z",
       "-w", "/workspace", "-e", "HOME=/home/node", "-e", "TERM=xterm-256color", "-e", "COLUMNS=140", "-e", "LINES=45",
       "--add-host", f"{RUM}:10.99.7.50", "-e", f"LLAMA_API_KEY={KEY}"]
for k, v in env.items():
    cmd += ["-e", f"{k}={v}"]
cmd += ["docker.io/library/node:22-slim", "/opt/qwen/bin/qwen", "--approval-mode", "yolo"]

subprocess.run(["podman", "rm", "-f", "-i", "proto-qwen-tui"], stderr=subprocess.DEVNULL, stdout=subprocess.DEVNULL)
pid, fd = pty.fork()
if pid == 0:
    os.execvp(cmd[0], cmd)
log = open(os.path.join(RES, f"{MODE}.tty"), "wb")

def pump(seconds, until=None):
    end = time.time() + seconds
    buf = b""
    while time.time() < end:
        r, _, _ = select.select([fd], [], [], 0.5)
        if r:
            try:
                data = os.read(fd, 65536)
            except OSError:
                return buf, True
            if not data:
                return buf, True
            log.write(data); log.flush(); buf += data
            if until and until in buf:
                return buf, False
    return buf, False

def send(text):
    os.write(fd, text.encode())

t0 = time.time()
out, dead = pump(30)
if EXPLORE:
    print(out.decode(errors="replace")[-3000:])
else:
    for step in ("Reply with exactly the word: pong", "Use your shell tool to run `ls` in the current directory and tell me how many files you see."):
        send(step); time.sleep(0.5); send("\r")
        pump(150, until=None if False else None) if False else None
        out, dead = pump(75)
        if dead:
            break
    send("/quit"); time.sleep(0.5); send("\r")
    out, dead = pump(15)
try:
    os.kill(pid, signal.SIGTERM)
except ProcessLookupError:
    pass
subprocess.run(["podman", "rm", "-f", "-i", "proto-qwen-tui"], stderr=subprocess.DEVNULL, stdout=subprocess.DEVNULL)
time.sleep(2)
subprocess.run(["pkill", "-x", "tcpdump"])
td.wait()
print(f"{MODE}: session {int(time.time()-t0)}s, pcap {os.path.getsize(pcap)} bytes")

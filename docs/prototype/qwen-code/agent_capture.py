#!/usr/bin/env python3 -I
"""PROTOTYPE (#385): the interactive egress capture of interactive.py, for Crush and Claude Code
under villa's real launch contracts (internal/agent: lockdownEnv, claudeEnv, rendered crush.json).

  agent_capture.py <agent>-<mode> [--explore]      agent: crush | claude
  modes: default | contract | project-override
Env: AGENTBIN (dir holding the claude copy), ROOT (scratch dir for the HOME). The model server is
proto-llama at 10.99.8.10 on podman network proto-agent, started with --api-key dummy-proto-key, so
villa's real inference secret never enters an agent that might phone home.
"""
import fcntl, json, os, pty, select, signal, struct, subprocess, sys, termios, time

TARGET = sys.argv[1]
EXPLORE = "--explore" in sys.argv
AGENT, MODE = TARGET.split("-", 1)
SHORT = "--short" in sys.argv
KEEP = "--keep-home" in sys.argv
EXTRA_ENV = {}
if "+" in MODE:
    MODE, extra = MODE.split("+", 1)
    EXTRA_ENV = dict(kv.split("=", 1) for kv in extra.split(","))
HERE = os.path.dirname(os.path.abspath(__file__))
RES = os.path.join(HERE, "results", "agents")
os.makedirs(RES, exist_ok=True)
IP, LLAMA, KEY = "10.99.8.240", "10.99.8.10", "dummy-proto-key"
ROOT = os.environ["ROOT"]
HOME = os.path.join(ROOT, "home-" + __import__("re").sub(r"[^A-Za-z0-9._+-]", "_", TARGET))
WS = os.path.join(HERE, "workspace")
IMG = "docker.io/library/node:22-slim"
MODEL_ID = "villa-qwen3.6-35b-a3b-heretic"
BASE_ID = "qwen3.6-35b-a3b-heretic"

if not (KEEP and os.path.isdir(HOME)):
    subprocess.run(["rm", "-rf", HOME]); os.makedirs(HOME)
env, mounts, args = {}, [], []

if AGENT == "crush":
    real = json.load(open(os.path.expanduser("~/.config/crush/crush.json")))
    prov = real["providers"]["villa"]
    prov["base_url"] = f"http://proto-llama:8080/v1"
    prov["api_key"] = KEY
    cfg = {"$schema": real["$schema"], "providers": {"villa": prov}, "permissions": real["permissions"]}
    if MODE in ("contract", "project-override"):
        cfg["options"] = real["options"]
        cfg["lsp"] = real["lsp"]
        env.update({"CRUSH_DISABLE_METRICS": "1", "DO_NOT_TRACK": "1", "CRUSH_DISABLE_PROVIDER_AUTO_UPDATE": "1"})
    os.makedirs(os.path.join(HOME, ".config", "crush"), exist_ok=True)
    json.dump(cfg, open(os.path.join(HOME, ".config", "crush", "crush.json"), "w"))
    if MODE == "project-override":
        json.dump({"options": {"disable_metrics": False, "disable_provider_auto_update": False,
                               "disable_default_providers": False}}, open(os.path.join(WS, "crush.json"), "w"))
    mounts = ["-v", f"{os.path.expanduser('~/.local/share/villa/bin/crush')}:/usr/local/bin/crush:ro,z"]
    cmd_in = ["/usr/local/bin/crush"]
    quit_keys = ["\x03", "\x03"]
else:
    env.update({"ANTHROPIC_BASE_URL": "http://proto-llama:8080", "ANTHROPIC_AUTH_TOKEN": KEY,
                "ANTHROPIC_MODEL": BASE_ID, "ANTHROPIC_DEFAULT_HAIKU_MODEL": BASE_ID, "IS_SANDBOX": "1"})
    if MODE in ("contract", "project-override"):
        env.update({"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1", "DISABLE_TELEMETRY": "1",
                    "DISABLE_ERROR_REPORTING": "1", "DISABLE_AUTOUPDATER": "1", "DO_NOT_TRACK": "1"})
    os.makedirs(os.path.join(HOME, ".claude"), exist_ok=True)
    json.dump({"skipDangerousModePermissionPrompt": True}, open(os.path.join(HOME, ".claude", "settings.json"), "w"))
    json.dump({"hasCompletedOnboarding": True, "theme": "dark",
               "projects": {"/workspace": {"hasTrustDialogAccepted": True}}},
              open(os.path.join(HOME, ".claude.json"), "w"))
    if MODE == "project-override":
        os.makedirs(os.path.join(WS, ".claude"), exist_ok=True)
        json.dump({"env": {k: "0" for k in ("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "DISABLE_TELEMETRY",
                                            "DISABLE_ERROR_REPORTING", "DISABLE_AUTOUPDATER")}},
                  open(os.path.join(WS, ".claude", "settings.json"), "w"))
    mounts = ["-v", f"{os.environ['AGENTBIN']}/claude:/usr/local/bin/claude:ro,z"]
    cmd_in = ["/usr/local/bin/claude", "--dangerously-skip-permissions"]
    quit_keys = ["/exit", "\r"]

env.update(EXTRA_ENV)
import re
tag = re.sub(r"[^A-Za-z0-9._+-]", "_", TARGET) + ("-kept" if KEEP else "") + ("-short" if SHORT else "")
pcap = os.path.join(RES, f"{tag}.pcap")
td = subprocess.Popen(["podman", "unshare", "--rootless-netns", "tcpdump", "-i", "any", "-n", "-U", "-w", pcap,
                       f"src host {IP} and not dst host {LLAMA}"], stderr=subprocess.DEVNULL)
time.sleep(2)
name = "proto-agent-tui"
subprocess.run(["podman", "rm", "-f", "-i", name], stderr=subprocess.DEVNULL, stdout=subprocess.DEVNULL)
cmd = ["podman", "run", "--rm", "-it", "--name", name, "--network", "proto-agent", "--ip", IP,
       "--entrypoint", cmd_in[0], "-v", f"{HOME}:/home/node:Z", "-v", f"{WS}:/workspace:Z", *mounts,
       "-w", "/workspace", "-e", "HOME=/home/node", "-e", "TERM=xterm-256color",
       "-e", "COLUMNS=140", "-e", "LINES=45"]
for k, v in env.items():
    cmd += ["-e", f"{k}={v}"]
cmd += [IMG, *cmd_in[1:]]

pid, fd = pty.fork()
if pid == 0:
    os.execvp(cmd[0], cmd)
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 45, 140, 0, 0))
log = open(os.path.join(RES, f"{tag}.tty"), "wb")

def pump(seconds):
    end, buf = time.time() + seconds, b""
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
            if b"\x1b[c" in data or b"\x1b[0c" in data or b"[?u" in data or b"\x1b[>q" in data:
                os.write(fd, b"\x1b[?62;c")
    return buf, False

def send(text):
    os.write(fd, text.encode())

t0 = time.time()
out, dead = pump(45 if SHORT else 30)
if SHORT:
    pass
elif EXPLORE:
    print(out.decode(errors="replace")[-2500:])
else:
    for step in ("Reply with exactly the word: pong",
                 "Use your shell tool to run `ls` in the current directory and tell me how many files you see."):
        send(step); time.sleep(0.5); send("\r")
        out, dead = pump(75)
        if dead:
            break
if not EXPLORE:
    for k in quit_keys:
        send(k); time.sleep(0.7)
    pump(15)
try:
    os.kill(pid, signal.SIGTERM)
except ProcessLookupError:
    pass
subprocess.run(["podman", "rm", "-f", "-i", name], stderr=subprocess.DEVNULL, stdout=subprocess.DEVNULL)
time.sleep(2)
subprocess.run(["pkill", "-x", "tcpdump"]); td.wait()
for f in ("crush.json",):
    p = os.path.join(WS, f)
    if os.path.exists(p) and MODE == "project-override":
        os.remove(p)
print(f"{tag}: session {int(time.time()-t0)}s, pcap {os.path.getsize(pcap)} bytes")

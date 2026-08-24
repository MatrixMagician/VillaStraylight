# VillaStraylight

A single Go control plane (`villa`) that fits a local AI stack to the machine it
is standing on, generates the units to run it, and proves it is actually running
on the GPU. The AI services themselves are integrated OSS containers, not
first-party code.

## Language

### The host and the fit

**Host profile**:
The typed picture of one machine — CPU, memory envelope, iGPU, kernel, ROCm
readiness — produced by probing it.
_Avoid_: system info, host facts, machine spec

**Memory envelope**:
The usable bytes a model, its KV cache, and headroom must all fit inside on this
host. Always written in full — bare "memory" is the knowledge feature, not this.
_Avoid_: RAM, VRAM, available memory, memory (unqualified)

**Typed Unknown**:
A probe result that could not be evaluated. Distinct from a confident negative:
an Unknown warns, a negative fails.
_Avoid_: null, missing, zero, n/a

**Recommendation**:
One memory-fitting choice of model, quant, context length, and inference
backend, with every term of the fit inequality shown.
_Avoid_: suggestion, config, defaults, profile

**Quant**:
The quantisation of a model's weights. Part of the recommendation, never
implied by the model name alone.
_Avoid_: precision, format, bit depth

**Catalog entry**:
One model in the shipped catalog — the only place a model name may come from.
_Avoid_: model definition, catalog item, listing

### The stack

**The stack**:
The set of running services — inference, chat, dashboard — plus the network and
volume they share.
_Avoid_: the app, the system, the services, the deployment

**Quadlet unit**:
A generated rootless Podman/systemd unit file. Always derived from config,
never the authority.
_Avoid_: service file, container spec, manifest

**Inference backend**:
Which GPU stack llama.cpp runs on — ROCm or Vulkan — and the image, flags,
device args and log markers that come with it. Always qualified.
_Avoid_: backend (unqualified), driver, runtime, GPU mode

**Resident set**:
The models held loaded at once, each in its own slot on its own loopback port,
instead of the inference unit restarting to trade one for another. Admission
decides what may join and what is evicted; holding is the opposite of swapping.
_Avoid_: model pool, loaded models, multi-model, hot models, swap

**Offload**:
Running the model's layers on the iGPU rather than the CPU.
_Avoid_: acceleration, GPU mode, hardware inference

**Residency proof**:
Evidence, from two independent signals, that offload actually happened. Absent
evidence is a warning; contradicted evidence is a failure.
_Avoid_: health check, liveness, GPU check

**CPU fallback**:
llama.cpp quietly running on the CPU instead. Always a failure — never reported
as a working stack.
_Avoid_: degraded mode, soft fallback, CPU mode

**Coding mode**:
The running stack flipped to a tool-calling configuration for the terminal
coding agent, and back again.
_Avoid_: agent mode, dev mode, tool mode

**Coder model**:
The model the coding agent talks to, distinct from the chat model.
_Avoid_: agent model, dev model

### Gates and proofs

**Preflight**:
The gate answering "can this host safely install the stack?"
_Avoid_: pre-check, validation, readiness check

**Doctor**:
The read-only gate answering "is this already-installed stack still healthy?"
_Avoid_: health check, diagnostics, status check

**Block / Warn**:
The two severities a gate result carries. Block stops the operation; Warn is
surfaced and passable.
_Avoid_: error/warning, fatal/nonfatal, critical/minor

**Swap**:
A transactional change to a running stack — of inference backend, model, or
coding mode — that captures state first and restores it verbatim on any failure.
_Avoid_: switch, change, update, migration

**pp / tg**:
Prompt-processing and token-generation throughput. Reported separately, never
blended into one number.
_Avoid_: tok/s, throughput, speed

### Knowledge and retrieval

**Memory**:
The vector store plus embedding service that give the chat UI persistent
knowledge. Bare "memory" is this feature; the fit constraint is always spelled
out as the "memory envelope".
_Avoid_: RAG, vector DB, knowledge store, memory stack

**Recall index**:
The indexed set of past chat transcripts the memory stack retrieves from.
_Avoid_: history, chat memory, conversation store

**Embedding skew**:
The recorded embedding identity of the recall index no longer matching the
configured one, making retrieval untrustworthy.
_Avoid_: index drift, stale embeddings, mismatch

**Staleness**:
How far the recall index has fallen behind the live chats. Unevaluable is a
distinct state from zero.
_Avoid_: lag, freshness, out of date

**Web guard**:
The classifier that flags prompt-injection patterns in fetched web content. It
flags and annotates; it never drops or rewrites content.
_Avoid_: filter, sanitiser, injection blocker, firewall

**Bounded outbound**:
The proven limit on what the stack may reach off-box — image and model pulls,
and nothing else.
_Avoid_: network policy, egress rules, firewall

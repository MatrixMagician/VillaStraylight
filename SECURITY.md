# Security policy

`villa` is the control plane for a local AI stack on one machine. Who it trusts,
the controls it relies on and the gaps it has accepted are in
[docs/THREAT_MODEL.md](docs/THREAT_MODEL.md).

## Supported versions

Only the latest release is supported. A fix ships as a new tag on `main`, and
older releases get no backports. The latest release is at the top of the
[releases page](https://github.com/MatrixMagician/VillaStraylight/releases).
`villa --version` prints the version you run.

## Report a vulnerability

Report it privately, as a GitHub security advisory for this repository:
<https://github.com/MatrixMagician/VillaStraylight/security/advisories/new>

Do not put the details in a public issue or pull request. If the advisory form
is not available to you, open an issue that says only that you have a security
report, and wait for a private channel.

## What to include

- The villa version (`villa --version`) and the host: Fedora release, kernel
  (`villa detect`), Podman version and inference backend (`villa status`).
- The subsystems that are on: memory, web search, coding agent, workspace agent,
  voice, image generation.
- The boundary you believe is crossed. The threat model's table names each one.
- Where the attacker starts: another local account, a web page the operator
  visits, fetched web content, a file in a workspace, a pulled image, or inside
  a task's sandbox.
- The exact commands that reproduce it, what you saw and what you expected.

Remove secrets first. `config.toml` holds `inference_secret`,
`web_loader_secret` and `searxng_secret`.

## What to expect

- The maintainer answers in the advisory thread. There is no response-time
  commitment.
- So far, each confirmed report was fixed in a tagged release and published as an
  advisory. Ten were published with v1.14 on 2026-09-22, GHSA-qxg9 and GHSA-gvp9
  among them.
- A report that restates an entry under "Known gaps and accepted residuals" in
  the threat model is already tracked. A way to make one of those gaps worse than
  the page says is a new report.

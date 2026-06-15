---
quick_id: 260615-qmr
slug: add-readme-prerequisites-section-documen
description: Add a README prerequisites section documenting Strix Halo kernel parameters
date: 2026-06-15
status: complete
one_liner: README gains a "Prerequisites: Kernel Parameters" section for Strix Halo unified-memory GRUB tuning.
key_files:
  modified:
    - README.md
---

# Summary — 260615-qmr

Added a `## Prerequisites: Kernel Parameters` section to `README.md`, placed
between `## Requirements` and `## Installation`, documenting the AMD Strix Halo
unified-memory kernel tuning:

- **Why custom kernel parameters** — unified dynamic memory lets the iGPU address
  ~124 GB on demand instead of statically partitioning RAM between CPU and GPU.
- **Performance note** — credits Lars Urban (Issue #66, linked): a 5–12% gain from
  `amd_iommu=off` vs. the previously recommended pass-through mode.
- **Apply the parameters** — the `GRUB_CMDLINE_LINUX` edit in `/etc/default/grub`,
  `sudo grub2-mkconfig -o /boot/grub2/grub.cfg`, and a reboot (added explicitly,
  since kernel params only take effect after reboot).
- **Parameter table** — `amd_iommu=off`, `amdgpu.gttsize=126976`,
  `ttm.pages_limit=32505856`, each with its unified-memory rationale, preserved
  verbatim from the source.

A closing line ties the GTT size to `villa detect`/`recommend` — the usable
envelope is the GTT total (`mem_info_gtt_total`), so raising `amdgpu.gttsize`
enlarges the budget the recommender fits a model against.

Doc-only change: no code, no tests touched. No other README content altered.

## Deviations

None. The only addition beyond the source text was the explicit `sudo reboot`
step (kernel parameters require a reboot) and the closing tie-in to the GTT-based
fit envelope.

---
quick_id: 260615-qmr
slug: add-readme-prerequisites-section-documen
description: Add a README prerequisites section documenting Strix Halo kernel parameters
date: 2026-06-15
mode: quick
status: complete
---

# Quick Task 260615-qmr: README kernel-parameters prerequisites section

## Goal

Document the AMD Strix Halo unified-memory kernel parameters as a prerequisites
section in `README.md`, so users prepare the host correctly before `villa install`.

## Task

**Files:** `README.md`

**Action:** Insert a new `## Prerequisites: Kernel Parameters` section between the
existing `## Requirements` and `## Installation` sections, containing:
- *Why custom kernel parameters?* — unified dynamic memory vs. static CPU/iGPU
  partitioning; the GPU can address ~124 GB on demand.
- A performance note crediting Lars Urban (Issue #66): a 5–12% gain from
  `amd_iommu=off` vs. the previously recommended pass-through mode.
- The `GRUB_CMDLINE_LINUX` edit + `grub2-mkconfig` + reboot procedure.
- A parameter table: `amd_iommu=off`, `amdgpu.gttsize=126976`,
  `ttm.pages_limit=32505856`, each with its unified-memory rationale.

**Verify:** Section renders as valid markdown; the three parameters and their
values appear verbatim; placement is between Requirements and Installation.

**Done:** `README.md` carries the section; technical facts preserved faithfully
from the source; no other README content altered.

## Notes

Doc-only change; no code, no tests. The closing line ties the GTT size to
`villa detect`/`recommend` (the usable envelope is GTT `mem_info_gtt_total`).

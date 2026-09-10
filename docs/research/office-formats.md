# Producing docx/xlsx inside the sandbox

Findings for [Research: producing docx/xlsx inside the sandbox](https://github.com/MatrixMagician/VillaStraylight/issues/165), on the map [Wayfinder map: v1.11, the workspace agent](https://github.com/MatrixMagician/VillaStraylight/issues/156).

The question: how a local model, driven through an agent harness inside the sandbox from the prototype addendum (a libkrun microVM on an internal no-egress network, one rw folder grant, no GPU, base image `registry.fedoraproject.org/fedora:44`), can create **and edit** Word and Excel files, and what that costs the image and the pin table. The sources are the libraries' own docs and source, LibreOffice's own command-line reference, Anthropic's help-centre pages, and the `docx`/`xlsx` skills Anthropic publishes in `anthropics/skills`, which are the instructions its production file-creation feature runs on. Where a claim rests on anything weaker it is marked **unverified**.

Every claim below was verified on 2026-09-10 on the gfx1151 dev box: the docs were fetched that day, the source was cloned at the SHAs given, and the round-trip, render and recalculation probes were run first on the host and then **inside a candidate image under `--runtime=krun` with `--network=none`**. The live villa stack was not touched. Scratch material (Containerfiles, probe scripts, outputs) is not committed; the commands are reproducible as given.

> **Placement note.** As with `update-version-checks.md`, this goes in `docs/research/` rather than the removed `.planning/` tree.

## Summary for the tickets waiting on this

1. **Anthropic drives a library, not a CLI, and the library is per-format.** Its `xlsx` skill writes `openpyxl` scripts; its `docx` skill writes `docx` (npm) scripts to *create* and edits the raw OOXML (`unzip` → edit `word/document.xml` → `zip`) to *modify*, because docx-js cannot open an existing file. LibreOffice is never the authoring tool: it is the renderer (`--convert-to pdf`), the formula engine (a recalculation macro), and the legacy-format converter. The fragile steps are pre-written scripts the model runs, not code it writes.
2. **Villa can do the same in Python only.** `python-docx` opens, edits and saves existing documents and leaves the parts it does not understand intact (footnotes, comments, floating text boxes survived a probe unchanged). `openpyxl` edits existing workbooks but drops what it does not model: shapes (documented), chart style/colour parts and `docProps/custom.xml` (observed), and **every cached formula value** (documented and observed). A LibreOffice recalculation pass restores the values; that pass is mandatory after any openpyxl write, exactly as Anthropic's skill mandates.
3. **The image cost is LibreOffice, and it is 1.1 GB.** `fedora:44` is 381 MB; adding `python3-openpyxl`, `python-docx` (pip, not packaged in Fedora 44), `python3-lxml` and `poppler-utils` makes 434 MB; adding `libreoffice-writer` + `libreoffice-calc` (which pull `libreoffice-core`, 346 MB on disk, and the Liberation/Carlito/Caladea/DejaVu fonts) makes **1.54 GB**. Fedora 44 has no `libreoffice-headless` subpackage; `libreoffice-core` provides it. No official LibreOffice OCI image was found in the sources consulted; the two third-party images that exist are a GUI-streaming desktop and an online-collaboration server, neither a converter.
4. **Verification works inside the microVM with no shim.** `soffice --headless --convert-to pdf` took 1.9 s (docx) and 1.1 s (xlsx) inside krun with no network; `pdftoppm` rendered the pages; Anthropic's `recalc.py` ran unmodified and filled in the cached values. `AF_UNIX` sockets work in the guest, so the `LD_PRELOAD` shim Anthropic's `soffice.py` compiles for its own sandbox is not needed. Two flags are load-bearing: `-env:UserInstallation=file:///tmp/…` (a fresh profile) and `--headless`.
5. **Everything is pinnable, but at different grains.** PyPI wheels carry sha256 digests; npm carries an sha512 integrity; Fedora RPMs carry an exact NEVR (`libreoffice-core-26.2.6.3-1.fc44`). The unit villa's pin table should carry is the **built sandbox image digest**, with the Containerfile pinning the package versions that produced it; that keeps the sandbox on the same `pins.Table` → `pinstate` → `villa update` path as every other image.
6. **Reading is a separate tool from writing.** Anthropic reads `.docx` with `pandoc -t markdown` and `.xlsx` with `markitdown`. Fedora 44 packages `pandoc-cli` 3.7.0.2 but not `markitdown`; `openpyxl` (two `load_workbook` passes) covers the xlsx read case without it.

## What Anthropic's own skills do, format by format

Source: `anthropics/skills` at `41bbe19d1a1a7eaab5e7bb9050a417e5c6cffc8f` (2026-09-03), files `skills/docx/SKILL.md`, `skills/xlsx/SKILL.md`, `skills/pptx/SKILL.md` and their `scripts/`. The README says these four document skills are the ones that "power Claude's document capabilities", and are source-available under a proprietary licence (`LICENSE.txt`: no copying, derivative works or redistribution). **Villa can learn from their design; it cannot ship them.**

| | Create | Edit existing | Read | Verify |
|---|---|---|---|---|
| `.docx` | `docx` (npm) script; "the model knows the API", the skill lists footguns only | `unzip` → edit `word/document.xml` in place → `zip`; helper scripts `merge_runs.py`, `comment.py`, `accept_changes.py`; XSD `validate.py` | `pandoc -t markdown` | `soffice --headless --convert-to pdf` → `pdftoppm -jpeg` → look at the images |
| `.xlsx` | `openpyxl` script; formulas, never computed values | `openpyxl` on the file, "match its conventions exactly" | `markitdown`; `openpyxl` two-pass for formulas + values | `recalc.py` (LibreOffice macro), then the same PDF → image loop |
| `.pptx` | `pptxgenjs` script | unzip → edit → zip, plus `add_slide.py`, `clean.py`, `thumbnail.py` | `markitdown` | `validate.py`, then PDF → image |

Three design facts carry over directly:

- **The model writes a small script against a library for the generative part, and runs pre-written, deterministic scripts for the parts that are easy to get subtly wrong.** The `docx` skill says why in its own terms: a `.docx` is a ZIP of XML, "docx-js cannot open existing files", and Word "splits text across many `<w:r>` runs" so a visible phrase often "doesn't exist as a contiguous string in the XML", which is what `merge_runs.py` exists to fix. Anthropic's engineering post on skills gives the general principle: "many applications require the deterministic reliability that only code can provide", so a skill bundles a script Claude "can run without loading either the script or the PDF into context".
- **LibreOffice is a converter and calculator, never an author.** `soffice` appears only as `--convert-to pdf/docx/pptx` and as the host of two Basic macros (`RecalculateAndSave`, `AcceptAllTrackedChanges`) that open, act, `store()` and close.
- **Verification is visual, and the skill says the render is approximate.** The `pptx` skill's typography section warns that LibreOffice "substitutes fonts it doesn't have" so "your QA preview can show text overflow (or fit) that the real deck won't have", and names the fonts whose substitutes are metric-safe: Arial, Calibri, Cambria, Times New Roman, Courier New, Bookman Old Style, Century Schoolbook.

What Anthropic's product claims, from its own help centre: Claude "can create Excel spreadsheets (.xlsx), PowerPoint presentations (.pptx), Word documents (.docx), and PDF files" in "an isolated, sandboxed container", using "common code packages"; Cowork "runs your tasks in the cloud" and, for local folders, "reaches it through the Claude Desktop app". Network egress off is described as the recommended starting posture, which is the sandbox this research targets.

## The libraries

| Library | Version, released | Runtime deps | Licence | Fedora 44 | Pin form |
|---|---|---|---|---|---|
| `python-docx` | 1.2.0, 2025-06-16 (`e454546…`, no newer tag) | `lxml>=3.1.0`, `typing_extensions>=4.9.0`; Python ≥3.9 | MIT | **not packaged** (`python3dist(python-docx)` has no provider); pip | wheel sha256 `3fd478f3…66c7`, sdist `7bc9d7b7…20ce` |
| `openpyxl` | 3.1.5, 2024-06-28 | `et-xmlfile`; Python ≥3.8; `defusedxml` recommended by its docs against XML bombs | MIT | `python3-openpyxl-3.1.5-5.fc44` | wheel sha256 `5282c12b…9de2`; RPM NEVR |
| `docx` (npm) | 9.7.1, 2026-05-27 (`fda088d…`) | `jszip`, `xml`, `xml-js`, `nanoid`, `hash.js`, `@types/node`; Node ≥10 | MIT | needs `nodejs22-22.23.1-2.fc44` (there is no `nodejs` package name) | `dist.integrity` sha512 `ilXFf9Mo…CsWg==` |
| LibreOffice | 26.2.6.3 (Fedora build) | on Fedora, `libreoffice-core` hard-depends on `java-25-openjdk-headless`, which lands in the image | MPL-2.0 and components (RPM licence field) | `libreoffice-core`, `-writer`, `-calc` 1:26.2.6.3-1.fc44 | RPM NEVR; image digest |

The upstream `openpyxl` repository is Mercurial on `foss.heptapod.net` and rejected a git clone, so its source claims here come from the released sdist and the docs at `openpyxl.readthedocs.io` (which still say 3.1.3 in their banner; PyPI's latest is 3.1.5). `python-docx`'s "Installing" page still lists Python 2.6–3.4 and `lxml >= 2.3.2`; the `pyproject.toml` in the tree is authoritative and is what the table shows.

**Recommendation: Python only.** `python-docx` + `openpyxl` cover create *and* edit for both formats from one runtime the base image already has. Adding Node for `docx` (npm) buys a nicer creation API for Word and nothing for editing; Anthropic itself falls back to raw XML for edits. A local model that must learn two ecosystems in a 64k-context harness is worse off than one that learns one.

## Round-tripping: what survives an edit

The question that matters for a workspace agent is not "can it create a file" but "can it change one cell in someone's workbook without wrecking the rest". Verified with a LibreOffice-authored `.docx` (flat-ODT source with a footnote, a comment and a floating text box, converted by `soffice --convert-to docx`) and a LibreOffice-rewritten `.xlsx` (the output of `recalc.py`, which stores the file from Calc).

### `python-docx` (open → edit one run → save)

- **Zip parts dropped: none. Footnote reference, comment reference and `txbxContent` all present in the saved `document.xml`, alongside the edit.** This matches the documented contract: "whatever is already in there will load and save just fine … `python-docx` is polite enough to leave them alone and smart enough to save them without actually understanding what they are." The same page is candid that it "only lets you make changes to existing documents" and that saving to the same name overwrites "without a peep".
- On a `python-docx`-authored file, changing a run's text kept its bold, 14 pt size, the section header and the table style; the PDF render confirms it.
- Documented limits: only inline pictures (floating shapes can be read past, not added); `.doc` is refused, convert first (`soffice --convert-to docx`, as the skill does).
- The run-fragmentation hazard the skill describes is real for Word-authored files and is why an edit tool should operate on `paragraph.runs` with a merge step, not on `paragraph.text` (which flattens formatting).

### `openpyxl` (load → change one input → save)

- **Zip parts dropped: `xl/charts/style1.xml`, `xl/charts/colors1.xml`, `xl/charts/_rels/chart1.xml.rels`, `docProps/custom.xml`** (`xl/sharedStrings.xml` was also removed but that is a representation change, strings are inlined). The chart itself (`chart1.xml`) survived and rendered with the new value. The docs' own warning is broader: "openpyxl does currently not read all possible items in an Excel file so shapes will be lost from existing files if they are opened and saved with the same name."
- **Every cached formula value is gone after save.** `D3` read back as `None` with `data_only=True`, both on a fresh openpyxl file and on the LibreOffice-written one. The docs: "openpyxl **never** evaluates formula". A downstream reader (pandas, a previewer, Open WebUI's document loader) sees blanks until something recalculates.
- `data_only=True` is destructive on save (formulas replaced by literals), `.xlsm` loses macros without `keep_vba=True`, and post-2007 functions need the `_xlfn.` prefix (all from the docs; the skill repeats each as a gotcha).
- The skill adds a hazard the docs do not: a workbook with **external links** loses them on openpyxl save + recalc, so `recalc.py` refuses unless `--force`.

**Consequence:** an xlsx edit is a two-step transaction, `openpyxl` write → LibreOffice recalculate, and the second step rewrites the file in place. Villa's capture → mutate → prove shape fits: capture the original, write, recalc, prove (`total_errors == 0` *and* a value spot-check), else restore. The skill's own warning applies to the prove step: "a green recalc proves your formulas *evaluate*, not that they are *right*."

### `docx` (npm)

Cannot open an existing document (skill, and the library's docs offer only a `patchDocument` that replaces `{{mustache}}` placeholders the *author* placed in advance). Creation-only.

## Verification: rendering and recalculation inside the microVM

LibreOffice's command-line reference documents `--convert-to OutputFileExtension[:OutputFilterName] [--outdir dir]` and `-env:VAR=VALUE` for "a non-default user profile path". Verified inside the candidate image under krun (`podman run --rm --runtime=krun --network=none --memory 4g -v …:/work:Z`):

```
kernel: 6.12.91  uid: 0
soffice: LibreOffice 26.2.6.3 620(Build:3)
docx -> pdf  real 1.88 s   (writer_pdf_Export)
xlsx -> pdf  real 1.07 s   (calc_pdf_Export)
recalc.py    {"status": "success", "total_errors": 0, "total_formulas": 2}
D3 after in-VM recalc: 185
unix socket: ok
egress: curl: (6) Could not resolve host: pypi.org
```

Facts a real unit must encode:

- **`-env:UserInstallation=file:///tmp/lo` is required.** Anthropic's `soffice.py` explains the failure mode: without a writable profile "soffice aborts with 'User installation could not be completed' and converts nothing". Under krun the guest is root (prototype addendum), so `$HOME` is writable, but a per-task temp profile is still the right shape: no state leaks between tasks and two conversions cannot fight over one profile.
- **No socket shim.** `soffice.py` compiles an `LD_PRELOAD` shim with `gcc` when `socket(AF_UNIX)` fails; the guest kernel allows it, so the image needs neither `gcc` nor the shim.
- **`--convert-to png` renders one page/sheet only** (observed: a single `edited.png`). Multi-page verification is PDF then `pdftoppm -jpeg -r N`, as the skill does; `poppler-utils` is 0.8 MB.
- **Fonts.** The image has 46 faces: Liberation (metric-compatible Arial/Times/Courier), **Carlito and Caladea** (`fc-match Calibri` → Carlito, `fc-match Cambria` → Caladea), DejaVu, Noto Sans. That covers exactly the "safe" set the pptx skill names for trustworthy overflow checks except Bookman/Century Schoolbook. Fonts written into a file are rendered by the *user's* Word/Excel, not by the sandbox; the render is a check on layout, not a promise of it.
- **Timing is per invocation**, and each `soffice` call cold-starts (the profile bootstrap is inside the 1–2 s). A task that renders after every edit pays that each time; acceptable against the 2–3.5 min tasks in `RESULTS.md`.

## What has to be in the image, and its size

Built from `registry.fedoraproject.org/fedora:44` (`sha256:669116f4…`, pulled 2026-09-10, 381 MB uncompressed) with `--setopt=install_weak_deps=False`:

| Layer | Adds | Image size |
|---|---|---|
| `fedora:44` | — | 381 MB |
| + `python3-openpyxl python3-lxml python3-pip poppler-utils`, `pip install --no-deps python-docx==1.2.0 typing_extensions` | 53 MB | 434 MB |
| + `libreoffice-writer libreoffice-calc liberation-fonts-all dejavu-sans-fonts` (pulls `libreoffice-core` and `java-25-openjdk-headless`; 398 RPMs total) | 1.10 GB | **1.54 GB** |

Notes: Fedora 44 has **no `libreoffice-headless`** package (it is provided by `libreoffice-core`, so `dnf install libreoffice-headless` resolves, but it is not a smaller thing). `python-docx` is not packaged, so the image needs `python3-pip` at build time and an egress-capable *build*; the running sandbox needs none. `pandoc-cli-3.7.0.2` is packaged if a Markdown reader for `.docx` is wanted; `markitdown` is not. Node is `nodejs22`, not needed under the Python-only recommendation.

**Third-party LibreOffice images**, for completeness: `lscr.io/linuxserver/libreoffice` is a Selkies desktop-streaming container (HTTPS GUI on 3001, "a terminal with passwordless sudo") on Alpine; `collabora/code` is the Collabora Online server (port 9980, `--privileged` recommended). Neither is a headless converter and neither is Fedora-based; both would be a second base image to pin. **Unverified:** whether The Document Foundation publishes an official OCI image, I found none in the LibreOffice help or download pages consulted, and did not search secondary sources.

## Pinnability for villa's pin table

| Component | Pin | Check mechanism (per `update-version-checks.md`) |
|---|---|---|
| sandbox image (built) | OCI digest of villa's own build | villa-published manifest (hybrid (c)), it is villa's artifact |
| `python-docx==1.2.0` | PyPI wheel sha256 (`pip install --require-hashes`) | PyPI JSON API, one GET |
| `openpyxl` | RPM NEVR `3.1.5-5.fc44` inside the image | Fedora repo metadata at build time |
| LibreOffice | RPM NEVR `1:26.2.6.3-1.fc44` inside the image | same |
| `docx` npm (if ever) | `dist.integrity` sha512 | npm registry JSON |

Two consequences. First, the *runtime* pin is one line, the image digest; the library versions are build inputs recorded in the Containerfile, the way `crush-policy.json` records a checksum. Second, a `dnf` build is not byte-reproducible, so "rebuild the image" yields a new digest under the same versions: the same rebuilt-versus-bumped ambiguity the earlier research found for `rocm-7.2.4`, and it should be reported the same way.

## Reproducing this

```bash
# clone the primary sources at the SHAs cited
git clone --depth 1 https://github.com/anthropics/skills            # 41bbe19d…
git clone --depth 1 https://github.com/python-openxml/python-docx    # e4545460…
git clone --depth 1 https://github.com/dolanmiu/docx                 # fda088d1…
curl -s https://pypi.org/pypi/python-docx/1.2.0/json | jq '.urls[].digests.sha256'
curl -s https://registry.npmjs.org/docx/latest | jq '.dist.integrity'
# build the candidate image, then probe it under krun with no network
podman build -t office-full -f Containerfile.office .    # the two-line Containerfile in the size table
podman run --rm --runtime=krun --network=none -v "$PWD/out:/work:Z" office-full \
  soffice -env:UserInstallation=file:///tmp/lo --headless --convert-to pdf --outdir /work /work/edited.docx
```

The round-trip probe is forty lines of `python-docx`/`openpyxl` (create, reopen, change one run or cell, save, diff the zip namelists); `recalc.py` is run from the skills checkout with `openpyxl` on the path.

## Decisions this unblocks

1. **Office formats are a Python-library capability, with LibreOffice as the verifier**, not a LibreOffice-driven one and not a Node one. The harness's tools are `python-docx` and `openpyxl` scripts plus three fixed helpers: render-to-images, recalculate, and (for docx) merge-runs. This is the office-formats line in the map's "Not yet specified".
2. **The sandbox image grows to ~1.5 GB and becomes a villa-built, digest-pinned image**, so it needs a row in `pins.Table` and a `villa update` subsystem entry; the Containerfile is the curated input. The alternative, a 434 MB image with no verifier, ships files the agent cannot look at.
3. **An xlsx write is a transaction**: openpyxl save → LibreOffice recalc → prove, with the original captured first, and the external-links refusal carried over. A docx write is a single save with a render for proof.
4. **The prototype that follows** should test the local model, not the libraries: can Qwen3.6 write a correct `openpyxl` script from a spec and read its own PDF render, in a 64k-context harness? Anthropic's skills assume a model that "knows the API" and needs only the footguns; whether a 35B-A3B model at Q4 does is the open question, and it is a grounding question of the T4 kind.
5. **Anthropic's skill files are a design reference, not a dependency.** Their licence forbids redistribution; villa's helpers must be its own, and the `recalc` macro and `soffice` invocation are small enough that that is no loss.

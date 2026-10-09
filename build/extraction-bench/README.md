# Extraction engine measurement (#314, ADR-0033)

The scripts that chose the document extractor and sized its reservation. They run
on the dev host against podman, LibreOffice, Pillow and the live `villa-embed`
(read-only, over `villa.network`); nothing here touches a villa unit or config.

| script | what it does |
|---|---|
| `make-fixtures.py OUT` | builds the three fixtures: an image-only PDF letter, a 60-row table PDF, a DOCX handbook. The copies in `internal/eval/docs/` are the run of 2026-10-08; LibreOffice stamps dates, so a rerun is not byte-identical. |
| `bench-direct.py OUT FIXTURES label=container[:3\|4\|docling] …` | PUTs each fixture straight at a running engine container (through its network namespace, so a `--network none` engine still answers) and records time, the fact hit and the cgroup peak. |
| `bench-owui.py OUT FIXTURES label [ENV=VALUE …]` | the end-to-end run: a scratch Open WebUI at villa's pinned digest uploads each fixture, reads the extracted text back and queries the file's collection with the question at k = 3. No env selects the default loaders. |
| `make-heavy.py OUT` | a 20-page 200-dpi scan and a 129-page text PDF for the footprint run. |
| `bench-footprint.py container 3\|4 DOC…` | PUTs the heavy documents, then three concurrent copies of the scan, and prints the cgroup peak and each JVM's high-water mark. |

## Results, 2026-10-08

Engines: `apache/tika:3.3.1.0-full`, `apache/tika:4.1.0-full`,
`docling-serve-cpu:v1.36.0`, each started with `--network none` for the direct run
(all three served every fixture with no network, so none downloads at start) and on
`villa.network` for the end-to-end run. Open WebUI 0.11.3 at the pinned digest,
`nomic-embed-text-v1.5` on the live embedder, Chroma in the scratch container, one
run per cell.

End to end (`bench-owui.py`): "hit" means a retrieved chunk matches the fact
pattern; the table pattern wants the part number and its price on one line.

| class | default | tika 3.3.1.0 | tika 4.1.0 | docling 1.36.0 |
|---|---|---|---|---|
| scanned-letter.pdf | refused: "The content provided is empty" | hit | hit | hit |
| table-parts.pdf | hit | hit | miss (near hit: cells on separate lines) | miss (row in a Markdown table, not in the top 3) |
| handbook.docx | hit | hit | hit | hit |

Direct (`bench-direct.py`, seconds and cgroup peak after the request): tika 3.3
letter 1.0 s / 341 MiB, table 0.2 s, docx 0.2 s; tika 4.1 letter 2.7 s / 570 MiB;
docling letter 4.1 s / 1851 MiB, table 10.1 s / 2072 MiB, docx 2.1 s.

Footprint (`bench-footprint.py`, tika 3.3.1.0 with `JAVA_TOOL_OPTIONS=-Xmx1g`):
idle 341 MiB peak; 129-page text PDF 0.4 s, peak 741 MiB; 20-page scan 37.4 s,
peak 952 MiB, child JVM VmHWM 786 MiB; three concurrent 20-page scans 38.5 s, peak
1245 MiB, child JVM VmHWM 906 MiB, parent 101 MiB.

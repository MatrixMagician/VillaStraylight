---
status: accepted
---

# Document extraction is a memory service on Apache Tika

Open WebUI turns an uploaded document into text with its own loaders: pypdf for a
PDF, python-docx for a DOCX. A scanned PDF has no text layer, so pypdf returns
nothing and Open WebUI refuses the upload with "The content provided is empty". The
document never reaches the vector store, and a question about it is answered from
nothing. Open WebUI can hand extraction to an external engine instead (#314), and the
two candidates it names, Apache Tika and Docling, were measured on the dev host before
one was chosen. ADR-0027 made a service's footprint one registry row; ADR-0028 settled
that a service villa pins carries a measured constant in its own package and that an
optional unit inside a subsystem carries its gate in the subsystem's unit registry.
This ADR applies both to the third such service.

## The measurement

Three fixture documents, each carrying one invented fact that a question asks for:
an image-only PDF of a letter (the scanned class), a three-page PDF price list of
sixty rows (the table class) and a DOCX handbook. Each engine was run end to end
through a scratch Open WebUI at villa's pinned digest (v0.11.3), joined to
`villa.network` to use the live `villa-embed` read-only, with the file uploaded, its
extracted text read back, and the file's collection queried with the question at
`k = 3`. One run per cell; the embedder and the vector query are deterministic, and
a second run of the OCR cells returned the same text.

| class | default loaders | Tika 3.3.1.0 | Tika 4.1.0 | Docling 1.36.0 |
|---|---|---|---|---|
| scanned PDF | upload refused, empty | hit | hit | hit |
| table PDF, row intact in a retrieved chunk | hit | hit | miss (cells on separate lines; near hit) | miss (row extracted, not retrieved) |
| DOCX | hit | hit | hit | hit |

The scanned class is the one the default cannot do at all: pypdf has no OCR, so the
zero is structural, not a close call. Tika 4.1.0 keeps a table's cells but puts each
on its own line, and none of its output forms (`tika/json/text`, the `text/plain`
Markdown form) keeps a row together for a PDF table. Docling kept the row in a
Markdown table, but that chunk was not among the three retrieved, and it costs a
5.1 GB image, 2 GB resident on the CPU and three times Tika's latency. Docling's
picture description could call villa's vision model, which would have needed the
authenticated route of ADR-0014; it was not needed, so no such route was built.

## Decision

- **The extractor is Apache Tika 3.3.1 with OCR, as `villa-extract`, inside the
  memory subsystem behind a derived gate.** `subsystem.ExtractOn(cfg)` is
  `MemoryOn(cfg) && cfg.Extractor`, the reranker's shape. It shares memory's proof and
  its update restart set: `verify memory` drives the upload path the extractor sits
  on, and `villa update memory` stops, snapshots, moves and starts it with Qdrant,
  the embedder and the reranker through `subsystem.Units(cfg)`. The flag exists for
  the reason the reranker's does: a memory-on host upgraded to this build must not
  re-render Open WebUI against a unit that is not running, so `villa install` writes
  `extractor = true` beside `memory_enabled`, and until it runs a memory-on config
  renders the stack it always had.

- **The image is a new pins component, `extractor`, pinned by digest.**
  `docker.io/apache/tika:3.3.1.0-full@sha256:d8e6ed96…` is a version tag (`3.3.1.0`),
  from the docker.io registry the table already allows. The `-full` variant carries
  tesseract, which is the whole gain. Nothing else in the stack serves this image, so
  unlike the reranker there is no existing component to resolve under; the signed pin
  manifest does not name it yet, and a manifest that omits a component leaves the
  compiled-in pin in force (the ADR-0022 precedent).

- **The footprint is 2 GiB, from a bound rather than a one-run peak.** The Tika
  image sets no heap limit, and a JVM's default is a quarter of host memory, 32 GB on
  the dev host. The unit sets `JAVA_TOOL_OPTIONS=-Xmx1g`, which reaches both the
  parent server and the child JVM it forks for parsing. At that flag, measured on the
  dev host on 2026-10-08: a cgroup peak of 952 MiB after one 20-page 200-dpi scan
  (37 s of OCR, about 2 s a page), 1245 MiB under three concurrent copies of it, and a
  child JVM high-water mark of 906 MiB. The heap cap, the JVM's native memory and up
  to three tesseract processes bound the unit near 1.9 GB, and the reservation is that
  bound. `memory.ExtractFootprintBytes` holds it; `recommend.ReservationsFor` lists it
  as the `extractor` row after the reranker.

- **Open WebUI gets the engine from the same gate.** With the extractor on, its unit
  gains `CONTENT_EXTRACTION_ENGINE=tika`, `TIKA_SERVER_URL=http://villa-extract:9998`
  and `TIKA_SERVER_VERSION=3`, after the reranker block. Its loader PUTs the file to
  `/tika/text` with the file's content type and reads `X-TIKA:content`; plain text
  files bypass the engine inside Open WebUI. The unit publishes no host port, mounts
  nothing, runs as the image's unprivileged user, and joins `villa.network` only.

- **`villa eval` gains an `extract` grader kind.** A case names a fixture document
  embedded in the suite, its content type and a pattern the extracted text must match;
  it is conducted through an extractor seam, never as a completion, and skipped with
  "extractor off" when the gate is off. The three fixtures are the three measured
  documents, and the table case's pattern asks for the part number and its price on
  one line, because row integrity is what separated the engines. The suite moves to
  version 3, and the suite hash now covers the fixtures, so an edited fixture fails
  the build like an edited case does.

- **Install proves it with a document, not a banner.** The memory readiness probe
  PUTs a plain-text sentence to `/tika/text` and requires the sentence back; the
  status row reads `GET /tika`, which answers a banner when the server is up.

## Consequences

- A memory-on host that re-runs `villa install` gets the extractor: an 863 MB image
  pull, a 2 GiB reservation off the chat-model fit, one more unit, and a document
  upload that goes through Tika. Until then nothing changes. `extractor = false` by
  hand turns the gate off at the next stack apply; the running unit stays until
  stopped, like every optional unit.
- The extractor runs no network at start and none at runtime: it was measured with
  `--network none` and extracted every fixture. It is reachable only by container DNS.
- Tika's child process gives a parse 120 s before it is killed and restarted, the
  server's default, which bounds a single OCR upload near sixty 200-dpi pages. Raising
  it means rendering a `tika-config.xml` into the container; nobody has hit the bound,
  so that is left for whoever does.
- The eval suite moves to version 3, which orphans baselines recorded under version 2
  until `villa eval --record` runs again (ADR-0018). Status and doctor report one more
  row and one more finding; their schemas go to 12 and 10.

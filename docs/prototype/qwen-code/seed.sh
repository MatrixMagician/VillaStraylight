#!/usr/bin/env bash
# PROTOTYPE — (re)creates ./workspace, a deliberately messy "Downloads-like" folder.
# WIPE ME: `rm -rf workspace` is always safe.
set -euo pipefail
cd "$(dirname "$0")"
rm -rf workspace && mkdir workspace && cd workspace
cat > CLAUDE.md <<'MD'
# Workspace rules
You are a local document assistant working ONLY inside this folder. No network, no code projects.
- Read before you write. Never delete a file unless the task says so explicitly; when a task
  says "report only", change nothing.
- Outputs are Markdown (.md) or CSV (.csv). Put every report you produce in reports/.
- Dates are ISO (YYYY-MM-DD). Money is GBP with two decimals.
- When finished, print a short summary of what you changed and what you were unsure about.
MD
cat > invoices_q2.csv <<'CSV'
invoice,client,date,amount_gbp,status
INV-1041,Tessier-Ashpool,2026-04-03,1200.00,sent
INV-1042,Sense/Net,2026-04-11,860.50,sent
INV-1043,Ono-Sendai,2026-04-28,2400.00,sent
INV-1044,Tessier-Ashpool,2026-05-06,1200.00,sent
INV-1045,Maas Biolabs,2026-05-19,3150.75,sent
INV-1046,Sense/Net,2026-06-02,860.50,sent
INV-1047,Ono-Sendai,2026-06-21,975.00,draft
CSV
cat > bank_export_may_jun.csv <<'CSV'
date,description,credit_gbp
2026-04-20,TESSIER ASHPOOL LTD INV1041,1200.00
2026-05-02,SENSE NET PAYMENT,860.50
2026-05-30,ONO SENDAI 1043,2400.00
2026-06-15,TESSIER ASHPOOL LTD INV1044,1200.00
2026-06-16,MAAS BIOLABS,3100.75
CSV
cat > "meeting 2026-05-04.txt" <<'TXT'
Weekly sync 4 May. Present: Case, Molly, Armitage.
- Molly to send the revised quote to Ono-Sendai by Friday.
- Case: migrate the shared drive to the new folder structure (blocked on Armitage approving the naming scheme).
- Armitage will confirm the Chiba trip dates.
- Decision: we drop the Sense/Net retainer renewal unless they agree to net-30.
TXT
cat > "meeting notes may 18.txt" <<'TXT'
Sync 18/05/2026. Case, Molly.
- Ono-Sendai accepted the quote; Molly to raise the invoice (done, INV-1045? check - might be Maas).
- Drive migration still blocked, Armitage away.
- Case to draft the Q2 client update memo.
- Action: chase Maas Biolabs, their payment came in short.
TXT
cat > "Sync-June-1.md" <<'MD'
# Sync 2026-06-01
Case, Molly, Armitage.
- Armitage approved the naming scheme: `<type>/<yyyy>-<mm>/<name>`.
- Case: drive migration unblocked, do it this week.
- Molly: Sense/Net agreed net-30, retainer renewed.
- Chiba trip 2026-07-08 to 2026-07-12.
- Open: the Q2 memo is still not drafted.
MD
cat > research_notes_local_ai.md <<'MD'
# Notes: running AI locally (raw)
- Strix Halo: 128 GB unified memory, so a 35B MoE at Q4 fits with room for a 128k context.
- ROCm 7.2.4 is the default backend now; Vulkan is the fallback if ROCm regresses.
- Nothing leaves the box: no telemetry, outbound only for image and model pulls.
- Open WebUI gives the chat surface; llama.cpp serves the model over an OpenAI-compatible API.
- Speculative decoding (ngram) roughly doubles tokens/s on repetitive output.
- Downsides: setup is fiddly, model quality lags the frontier, no mobile app.
MD
cp research_notes_local_ai.md "research_notes_local_ai (1).md"
cp invoices_q2.csv "invoices_q2 - Copy.csv"
printf 'IMG_20260601_101512.jpg placeholder\n' > IMG_20260601_101512.jpg
printf 'IMG_20260601_101530.jpg placeholder\n' > IMG_20260601_101530.jpg
printf 'to do: renew domain, book chiba hotel, send memo\n' > todo.txt
printf 'draft letter to Maas Biolabs re short payment - TODO\n' > letter_draft.txt
echo "seeded $(ls | wc -l) files in $(pwd)"

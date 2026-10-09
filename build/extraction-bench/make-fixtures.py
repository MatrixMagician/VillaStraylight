#!/usr/bin/env python3
"""Build the three extraction fixtures: a scanned (image-only) PDF, a table PDF
and a DOCX. Each carries one invented fact a question asks for. Deterministic
inputs; LibreOffice output carries timestamps, so the fixtures are pinned once.

usage: make-fixtures.py OUTDIR
"""
import pathlib
import random
import subprocess
import sys

from PIL import Image, ImageDraw, ImageFont

out = pathlib.Path(sys.argv[1])
out.mkdir(parents=True, exist_ok=True)
work = out / "work"
work.mkdir(exist_ok=True)

FONT = "/usr/share/fonts/liberation-sans-fonts/LiberationSans-Regular.ttf"

# 1. Scanned letter: text rendered to a page image, tilted, wrapped as PDF.
letter = [
    "HOLLINS ROAD DEPOT",
    "Notice of annual boiler inspection",
    "",
    "To all site leads,",
    "",
    "The annual boiler inspection at the Hollins Road depot is",
    "scheduled for 14 March 2027, starting at 09:30. The inspector",
    "is Ms Dagny Okafor of Weald Pressure Services.",
    "",
    "The plant room must be cleared of stored material by 08:45 on",
    "the day, and the two feed pumps isolated at the local panel.",
    "Boiler 2 stays in service for the north wing until the",
    "inspector releases it.",
    "",
    "Please confirm attendance to the depot office by 1 March.",
    "",
    "R. Castellanos",
    "Depot engineer",
]
dpi = 150
page = Image.new("L", (int(8.27 * dpi), int(11.69 * dpi)), 255)
draw = ImageDraw.Draw(page)
font = ImageFont.truetype(FONT, int(dpi * 12 / 72))
y = int(1.2 * dpi)
for line in letter:
    draw.text((int(1.1 * dpi), y), line, font=font, fill=20)
    y += int(dpi * 12 / 72 * 1.55)
rnd = random.Random(314)
px = page.load()
for _ in range(4000):
    px[rnd.randrange(page.width), rnd.randrange(page.height)] = rnd.randrange(120, 230)
page = page.rotate(0.6, resample=Image.BICUBIC, fillcolor=255)
page.save(out / "scanned-letter.pdf", "PDF", resolution=dpi)

# 2. Table PDF: an HTML price list, converted by LibreOffice.
rows = []
_desc = ["Inlet strainer", "Pressure gauge", "Shaft sleeve", "Bearing housing cover", "Coupling insert",
         "Drain plug with washer", "Volute casing bolt set", "Wear ring, front", "Wear ring, rear",
         "Lantern ring", "Gland follower", "Impeller key", "Casing wear plate", "Suction flange gasket"]
_rnd = random.Random(220)
for i in range(60):
    num = 100 + i * 7
    if num == 219:
        num = 220
    desc = _desc[i % len(_desc)]
    price = f"{_rnd.randrange(400, 26000) / 100:.2f}"
    lead = f"{_rnd.randrange(1, 14)} days"
    if num == 220:
        desc, price, lead = "Pump gasket set", "41.80", "3 days"
    rows.append((f"VX-{num}", desc, price, lead))
assert ("VX-220", "Pump gasket set", "41.80", "3 days") in rows
trs = "\n".join(
    f"<tr><td>{a}</td><td>{b}</td><td>{c}</td><td>{d}</td></tr>" for a, b, c, d in rows
)
table_html = f"""<html><head><meta charset="utf-8"><title>Spare parts price list</title></head>
<body>
<h1>Spare parts price list, revision 7</h1>
<p>Prices in euros, excluding tax. Lead time is from order confirmation.</p>
<table border="1" cellpadding="4">
<tr><th>Part number</th><th>Description</th><th>Unit price</th><th>Lead time</th></tr>
{trs}
</table>
<p>Orders over 500 euros ship free of charge.</p>
</body></html>"""
(work / "table-parts.html").write_text(table_html)

# 3. DOCX handbook: HTML converted by LibreOffice to Word 2007 XML.
handbook_html = """<html><head><meta charset="utf-8"><title>Field handbook</title></head>
<body>
<h1>Field handbook</h1>
<h2>4. Cold-weather start</h2>
<p>Below an ambient temperature of -12 °C the engine must not be cranked cold.</p>
<p>Run the pre-heat timer for 6 minutes before the first crank, and for 9 minutes
when the unit has stood for more than a week.</p>
<p>If the glow indicator stays lit after the timer ends, abort the start and log
a fault against the pre-heat relay.</p>
<h2>5. Fuel</h2>
<p>Winter-grade fuel is required from November to March.</p>
</body></html>"""
(work / "handbook.docx.html").write_text(handbook_html)

subprocess.run(
    ["soffice", "--headless", "--convert-to", "pdf:writer_web_pdf_Export",
     "--outdir", str(out), str(work / "table-parts.html")],
    check=True, capture_output=True)
subprocess.run(
    ["soffice", "--headless", "--convert-to", "docx:MS Word 2007 XML",
     "--outdir", str(out), str(work / "handbook.docx.html")],
    check=True, capture_output=True)
(out / "handbook.docx.docx").rename(out / "handbook.docx")

for f in sorted(out.iterdir()):
    if f.is_file():
        print(f"{f.name}\t{f.stat().st_size} bytes")

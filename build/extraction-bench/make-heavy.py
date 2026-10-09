#!/usr/bin/env python3
"""Heavier documents for the footprint measurement: a 20-page scanned PDF, a
long text PDF and a large DOCX with embedded images.

usage: make-heavy.py OUTDIR
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
rnd = random.Random(99)
words = ("depot boiler inspection pump gasket impeller seal bearing coupling flange "
         "strainer gauge valve pressure flow relay timer fuel winter handbook notice").split()


def para(n):
    return " ".join(rnd.choice(words) for _ in range(n))


dpi = 200
font = ImageFont.truetype(FONT, int(dpi * 11 / 72))
pages = []
for p in range(20):
    img = Image.new("L", (int(8.27 * dpi), int(11.69 * dpi)), 255)
    d = ImageDraw.Draw(img)
    y = int(0.9 * dpi)
    for _ in range(48):
        d.text((int(0.9 * dpi), y), para(11).capitalize() + ".", font=font, fill=15)
        y += int(dpi * 11 / 72 * 1.5)
    d.text((int(7 * dpi), int(11 * dpi)), f"page {p + 1}", font=font, fill=15)
    pages.append(img.rotate(rnd.uniform(-0.5, 0.5), resample=Image.BICUBIC, fillcolor=255))
pages[0].save(out / "scan-20p.pdf", "PDF", resolution=dpi, save_all=True, append_images=pages[1:])

html = ["<html><body>"]
for i in range(600):
    html.append(f"<h3>Section {i}</h3><p>{para(120)}</p>")
html.append("</body></html>")
(work / "long.html").write_text("\n".join(html))
subprocess.run(["soffice", "--headless", "--convert-to", "pdf:writer_web_pdf_Export", "--outdir", str(out),
                str(work / "long.html")], check=True, capture_output=True)

imgs = []
for i in range(12):
    im = Image.new("RGB", (1600, 1200))
    px = im.load()
    for x in range(0, 1600, 4):
        for y in range(0, 1200, 4):
            c = (rnd.randrange(256), rnd.randrange(256), rnd.randrange(256))
            for dx in range(4):
                for dy in range(4):
                    px[x + dx, y + dy] = c
    p = work / f"img{i}.png"
    im.save(p)
    imgs.append(p)
html = ["<html><body><h1>Illustrated report</h1>"]
for i, p in enumerate(imgs):
    html.append(f"<p>{para(80)}</p><img src=\"{p.name}\" width=\"600\">")
html.append("</body></html>")
(work / "big.docx.html").write_text("\n".join(html))
subprocess.run(["soffice", "--headless", "--convert-to", "docx:MS Word 2007 XML", "--outdir", str(out),
                str(work / "big.docx.html")], check=True, capture_output=True)
(out / "big.docx.docx").rename(out / "big.docx")
for f in sorted(out.iterdir()):
    if f.is_file():
        print(f"{f.name}\t{f.stat().st_size} bytes")

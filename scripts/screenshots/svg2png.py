#!/usr/bin/env python3
"""Render the SVG screenshots made by ansi2svg.py to PNG with Pillow.

Characters are drawn one by one on the terminal grid, so the result lines up
exactly regardless of font metrics.

usage: svg2png.py out_dir file.svg [file.svg ...] [--scale 2]
"""
import argparse
import html
import pathlib
import re

from PIL import Image, ImageDraw, ImageFont

FONT_DIRS = ["/usr/share/fonts/truetype/dejavu", "/usr/share/fonts/TTF", "/usr/share/fonts/dejavu"]
NUM = r"(-?[\d.]+)"
ATTR = re.compile(r'(\w[\w-]*)="([^"]*)"')


def font(name, size):
    for d in FONT_DIRS:
        p = pathlib.Path(d) / name
        if p.exists():
            return ImageFont.truetype(str(p), size)
    raise SystemExit(f"font {name} not found (install fonts-dejavu-core)")


def render(svg: str, scale: float) -> Image.Image:
    root = dict(ATTR.findall(re.search(r"<svg ([^>]*)>", svg).group(1)))
    w, h = float(root["width"]), float(root["height"])
    base = float(root.get("font-size", 14))
    img = Image.new("RGB", (round(w * scale), round(h * scale)), "#000000")
    d = ImageDraw.Draw(img)
    fonts = {}

    def f(size, bold):
        key = (size, bold)
        if key not in fonts:
            fonts[key] = font("DejaVuSansMono-Bold.ttf" if bold else "DejaVuSansMono.ttf", round(size * scale))
        return fonts[key]

    for tag, attrs, body in re.findall(r"<(rect|circle|text)\s([^>]*?)/?>(?:([^<]*)</text>)?", svg):
        a = dict(ATTR.findall(attrs))
        s = lambda k, dflt=0.0: float(a.get(k, dflt)) * scale
        if tag == "rect":
            if a.get("width") == "100%":
                d.rounded_rectangle([0, 0, img.width - 1, img.height - 1], radius=s("rx"), fill=a["fill"])
            else:
                d.rectangle([s("x"), s("y"), s("x") + s("width"), s("y") + s("height")], fill=a["fill"])
        elif tag == "circle":
            r = s("r")
            d.ellipse([s("cx") - r, s("cy") - r, s("cx") + r, s("cy") + r], fill=a["fill"])
        elif tag == "text":
            text = html.unescape(body)
            size = float(a.get("font-size", base))
            fnt = f(size, a.get("font-weight") == "bold")
            x, y = s("x"), s("y")
            if a.get("text-anchor") == "middle":
                d.text((x, y), text, fill=a["fill"], font=fnt, anchor="ms")
                continue
            n = max(1, len(text))
            cw = s("textLength") / n if "textLength" in a else fnt.getlength("M")
            for i, ch in enumerate(text):
                if ch != " ":
                    d.text((x + i * cw, y), ch, fill=a["fill"], font=fnt, anchor="ls")
    return img


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("out_dir", type=pathlib.Path)
    ap.add_argument("svgs", nargs="+", type=pathlib.Path)
    ap.add_argument("--scale", type=float, default=2.0)
    a = ap.parse_args()
    a.out_dir.mkdir(parents=True, exist_ok=True)
    for p in a.svgs:
        out = a.out_dir / (p.stem + ".png")
        render(p.read_text(encoding="utf-8"), a.scale).save(out, optimize=True)
        print(out)


if __name__ == "__main__":
    main()

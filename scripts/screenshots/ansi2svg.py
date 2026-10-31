#!/usr/bin/env python3
"""Convert a `tmux capture-pane -e` dump into an SVG "screenshot" (stdlib only).

usage: ansi2svg.py input.ansi output.svg [--cols 150] [--title text]
"""
import argparse
import re
from xml.sax.saxutils import escape

# Tango-like 16 colour palette, close to a classic dark terminal.
PALETTE = [
    "#000000", "#cc0000", "#4e9a06", "#c4a000", "#3465a4", "#75507b", "#06989a", "#d3d7cf",
    "#555753", "#ef2929", "#8ae234", "#fce94f", "#729fcf", "#ad7fa8", "#34e2e2", "#eeeeec",
]
FG, BG = "#d3d7cf", "#16181a"
CW, CH, FONT = 8.4, 17.0, 14  # cell width/height, font size
PAD = 14
SGR = re.compile(r"\x1b\[([0-9;]*)m")


def xterm256(n):
    if n < 16:
        return PALETTE[n]
    if n < 232:
        n -= 16
        lv = [0, 95, 135, 175, 215, 255]
        return "#%02x%02x%02x" % (lv[n // 36], lv[(n // 6) % 6], lv[n % 6])
    g = 8 + (n - 232) * 10
    return "#%02x%02x%02x" % (g, g, g)


def parse(text, cols):
    """Return rows of cells: (char, fg, bg, bold)."""
    fg, bg, bold, rev = None, None, False, False
    rows = []
    for line in text.rstrip("\n").split("\n"):
        row = []
        pos = 0
        for m in SGR.finditer(line + "\x1b[m"):
            for ch in line[pos:m.start()]:
                f, b = (bg, fg) if rev else (fg, bg)
                row.append((ch, f, b, bold))
            pos = m.end()
            codes = [int(c) if c else 0 for c in m.group(1).split(";")] if m.group(1) else [0]
            i = 0
            while i < len(codes):
                c = codes[i]
                if c == 0:
                    fg, bg, bold, rev = None, None, False, False
                elif c == 1:
                    bold = True
                elif c == 22:
                    bold = False
                elif c == 7:
                    rev = True
                elif c == 27:
                    rev = False
                elif 30 <= c <= 37:
                    fg = PALETTE[c - 30]
                elif 90 <= c <= 97:
                    fg = PALETTE[c - 90 + 8]
                elif c == 39:
                    fg = None
                elif 40 <= c <= 47:
                    bg = PALETTE[c - 40]
                elif 100 <= c <= 107:
                    bg = PALETTE[c - 100 + 8]
                elif c == 49:
                    bg = None
                elif c in (38, 48) and i + 2 < len(codes) and codes[i + 1] == 5:
                    col = xterm256(codes[i + 2])
                    if c == 38:
                        fg = col
                    else:
                        bg = col
                    i += 2
                elif c in (38, 48) and i + 4 < len(codes) and codes[i + 1] == 2:
                    col = "#%02x%02x%02x" % tuple(codes[i + 2:i + 5])
                    if c == 38:
                        fg = col
                    else:
                        bg = col
                    i += 4
                i += 1
        rows.append(row[:cols])
    return rows


def runs(row, key):
    """Group consecutive cells with equal key -> (start, cells)."""
    out, start = [], 0
    for i in range(1, len(row) + 1):
        if i == len(row) or key(row[i]) != key(row[start]):
            out.append((start, row[start:i]))
            start = i
    return out


def render(rows, cols, title):
    width = cols * CW + 2 * PAD
    top = PAD + 22
    height = len(rows) * CH + top + PAD
    o = [
        f'<svg xmlns="http://www.w3.org/2000/svg" width="{width:.0f}" height="{height:.0f}" '
        f'viewBox="0 0 {width:.0f} {height:.0f}" font-family="DejaVu Sans Mono, Menlo, Consolas, monospace" '
        f'font-size="{FONT}">',
        f'<rect width="100%" height="100%" rx="8" fill="{BG}"/>',
        f'<circle cx="{PAD + 6}" cy="{PAD + 4}" r="5" fill="#555753"/>',
        f'<circle cx="{PAD + 22}" cy="{PAD + 4}" r="5" fill="#555753"/>',
        f'<circle cx="{PAD + 38}" cy="{PAD + 4}" r="5" fill="#555753"/>',
        f'<text x="{width / 2:.1f}" y="{PAD + 9}" fill="#888a85" text-anchor="middle" font-size="12">{escape(title)}</text>',
    ]
    for y, row in enumerate(rows):
        ty = top + y * CH
        for start, cells in runs(row, lambda c: c[2]):
            if cells and cells[0][2]:
                o.append(f'<rect x="{PAD + start * CW:.1f}" y="{ty:.1f}" width="{len(cells) * CW:.1f}" '
                         f'height="{CH:.1f}" fill="{cells[0][2]}"/>')
        for start, cells in runs(row, lambda c: (c[1], c[3])):
            text = "".join(c[0] for c in cells)
            if not text.strip():
                continue
            fg = cells[0][1] or FG
            weight = ' font-weight="bold"' if cells[0][3] else ""
            o.append(f'<text x="{PAD + start * CW:.1f}" y="{ty + CH * 0.78:.1f}" fill="{fg}"{weight} '
                     f'textLength="{len(cells) * CW:.1f}" lengthAdjust="spacingAndGlyphs" '
                     f'xml:space="preserve">{escape(text)}</text>')
    o.append("</svg>")
    return "\n".join(o)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("input")
    ap.add_argument("output")
    ap.add_argument("--cols", type=int, default=150)
    ap.add_argument("--title", default="proctab")
    a = ap.parse_args()
    with open(a.input, encoding="utf-8", errors="replace") as f:
        rows = parse(f.read(), a.cols)
    with open(a.output, "w", encoding="utf-8") as f:
        f.write(render(rows, a.cols, a.title))


if __name__ == "__main__":
    main()

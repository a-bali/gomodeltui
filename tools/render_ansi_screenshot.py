#!/usr/bin/env python3
"""Rasterize ANSI terminal output with a monospace system font (Pillow)."""
import re
import sys
from PIL import Image, ImageDraw, ImageFont

text = open(sys.argv[1], encoding="utf-8").read()
font_path = "/System/Library/Fonts/Supplemental/Menlo.ttc"
font = ImageFont.truetype(font_path, 15, index=0)
cell_w, cell_h = 9, 21
lines = text.splitlines()
width = max((len(re.sub(r"\x1b\[[0-9;]*m", "", line)) for line in lines), default=100)
image = Image.new("RGB", (width * cell_w + 40, len(lines) * cell_h + 40), "#111318")
draw = ImageDraw.Draw(image)
palette = ["#000000", "#e06c75", "#98c379", "#e5c07b", "#61afef", "#c678dd", "#56b6c2", "#d7dae0",
           "#5c6370", "#e06c75", "#98c379", "#e5c07b", "#61afef", "#c678dd", "#56b6c2", "#ffffff"]
fg, bg = "#d7dae0", "#111318"

def color(code):
    if code < 16:
        return palette[code]
    if code >= 232:
        v = 8 + (code - 232) * 10
        return "#%02x%02x%02x" % (v, v, v)
    code -= 16
    levels = [0, 95, 135, 175, 215, 255]
    return "#%02x%02x%02x" % (levels[code // 36], levels[(code // 6) % 6], levels[code % 6])

for y, raw in enumerate(lines):
    x = 0
    i = 0
    while i < len(raw):
        if raw[i] == "\x1b":
            match = re.match(r"\x1b\[([0-9;]*)m", raw[i:])
            if match:
                values = [int(v or 0) for v in match.group(1).split(";")]
                j = 0
                while j < len(values):
                    v = values[j]
                    if v == 0:
                        fg, bg = "#d7dae0", "#111318"
                    elif v == 39:
                        fg = "#d7dae0"
                    elif v == 49:
                        bg = "#111318"
                    elif v in (38, 48) and j + 2 < len(values):
                        target = "fg" if v == 38 else "bg"
                        if values[j + 1] == 5:
                            value = color(values[j + 2]); j += 2
                        elif values[j + 1] == 2 and j + 4 < len(values):
                            value = "#%02x%02x%02x" % tuple(values[j + 2:j + 5]); j += 4
                        else:
                            j += 1; continue
                        if target == "fg": fg = value
                        else: bg = value
                    elif 30 <= v <= 37:
                        fg = palette[v - 30]
                    elif 90 <= v <= 97:
                        fg = palette[v - 90 + 8]
                    elif 40 <= v <= 47:
                        bg = palette[v - 40]
                    elif 100 <= v <= 107:
                        bg = palette[v - 100 + 8]
                    j += 1
                i += len(match.group(0)); continue
            i += 1; continue
        char = raw[i]
        if char == "\t":
            x += 4
        elif char >= " " and char != "\x7f":
            px, py = 20 + x * cell_w, 20 + y * cell_h
            draw.rectangle((px, py, px + cell_w, py + cell_h), fill=bg)
            draw.text((px, py + 1), char, font=font, fill=fg, stroke_width=0)
            x += 2 if ord(char) > 0xFFFF else 1
        i += 1

image.save(sys.argv[2])

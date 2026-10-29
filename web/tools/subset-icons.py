#!/usr/bin/env python3
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
"""Subset the Material Symbols (Apache-2.0) variable font to the icons the app uses.

Scans src/ (and Quasar's own icon set) for `sym_o_<name>`, freezes the variable axes to the regular, unfilled, 24 px design,
keeps only those ligatures and writes src/css/symbols.woff2. The full font is 3.6 MB; the subset is a few KB.
Needs: python3-fonttools python3-brotli (apt) and `npm ci` done (the font comes from node_modules).
"""
import glob
import os
import re
import sys

from fontTools import subset
from fontTools.ttLib import TTFont
from fontTools.varLib import instancer

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
names = set()
srcs = glob.glob(os.path.join(ROOT, "src", "**", "*.vue"), recursive=True) + glob.glob(os.path.join(ROOT, "src", "**", "*.js"), recursive=True) + glob.glob(os.path.join(ROOT, "src", "**", "*.ts"), recursive=True)
srcs.append(os.path.join(ROOT, "node_modules", "quasar", "icon-set", "material-symbols-outlined.js"))
for f in srcs:
    if os.path.exists(f):
        names.update(re.findall(r"sym_o_([a-z0-9_]+)", open(f, encoding="utf-8").read()))
if not names:
    sys.exit("no icon names found")

fonts = sorted(glob.glob(os.path.join(ROOT, "node_modules", "@quasar", "extras", "material-symbols-outlined", "web-font", "*.woff2")), key=os.path.getsize)
if not fonts:
    sys.exit("material symbols font not found: run npm ci first")
font = TTFont(fonts[-1])
axes = {a.axisTag: a.defaultValue for a in font["fvar"].axes}
axes.update({"wght": 400, "FILL": 0, "GRAD": 0, "opsz": 24})
font = instancer.instantiateVariableFont(font, {k: v for k, v in axes.items() if k in {a.axisTag for a in font["fvar"].axes}})

# keep only the ligatures that spell a wanted icon name (otherwise every icon whose letters are present would stay)
cmap = font.getBestCmap()
rev = {}
for cp, g in sorted(cmap.items()):
    rev.setdefault(g, chr(cp))
    if chr(cp) in "abcdefghijklmnopqrstuvwxyz0123456789_":  # the font maps A and a to one glyph: the lower-case name is what the icons spell
        rev[g] = chr(cp)
gsub = font["GSUB"].table
kept_chars = set()
kept_names = set()
for lookup in gsub.LookupList.Lookup:
    for st in lookup.SubTable:
        if st.LookupType == 7:
            st = st.ExtSubTable
        if st.LookupType != 4:
            continue
        for first in list(st.ligatures):
            keep = []
            for lig in st.ligatures[first]:
                seq = rev.get(first, "?") + "".join(rev.get(c, "?") for c in lig.Component)
                if seq in names:
                    keep.append(lig)
                    kept_chars.update(seq)
                    kept_names.add(seq)
            if keep:
                st.ligatures[first] = keep
            else:
                del st.ligatures[first]

opts = subset.Options()
opts.layout_features = ["liga", "rlig", "calt"]
opts.flavor = "woff2"
opts.notdef_outline = True
opts.hinting = False
opts.drop_tables += ["GPOS", "GDEF", "STAT", "MVAR", "HVAR", "avar", "fvar", "gvar", "cvar"]
sub = subset.Subsetter(opts)
sub.populate(text="".join(sorted(kept_chars)))
sub.subset(font)
out = os.path.join(ROOT, "src", "css", "symbols.woff2")
font.flavor = "woff2"
font.save(out)
print(f"{len(names)} icon names scanned, {os.path.getsize(out)} bytes written to {os.path.relpath(out, ROOT)}")
missing = sorted(names - kept_names)
if missing:
    print("warning: no such icon in the font:", ", ".join(missing))

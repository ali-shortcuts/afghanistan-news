#!/usr/bin/env python3
"""Static sanity checks for the Android source tree.

This workspace has no Android SDK/JDK 17, so the module cannot be compiled here. These checks
cover the failure modes that a compiler would catch first and that are cheap to detect:

  1. every `import com.afghanistan.news...` resolves to a declared symbol or package
  2. every `R.<type>.<name>` reference exists in res/
  3. every `@style/@string/@drawable/@color/@xml/@mipmap` referenced from XML exists
  4. Kotlin files are inside the directory that matches their package

Run:  python3 tools/check_sources.py
"""
from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SRC = ROOT / "app/src/main/java"
RES = ROOT / "app/src/main/res"
BASE_PKG = "com.afghanistan.news"

# Top-level declarations, including extension functions (`fun ArticleDto.toEntity()`) and
# generic functions (`suspend fun <T> safeApiCall()`).
DECL = re.compile(
    r"^(?:@\w+\s+)?(?:public |internal |private |abstract |open |sealed |data |enum |annotation |value |"
    r"suspend |inline |operator |override |external |expect |actual |const )*"
    r"(?:class|interface|object|fun|val|var|typealias)\s+"
    r"(?:<[^>]+>\s*)?"
    r"(?:[A-Za-z_][A-Za-z0-9_.]*\.)?([A-Za-z_][A-Za-z0-9_]*)",
    re.M,
)
IMPORT = re.compile(rf"^import\s+({BASE_PKG}(?:\.[A-Za-z_][A-Za-z0-9_]*)+)\s*$", re.M)
R_REF = re.compile(r"\bR\.(string|drawable|color|style|xml|mipmap|array|plurals|integer|bool)\.([A-Za-z0-9_]+)")
XML_REF = re.compile(r"@(string|drawable|color|style|xml|mipmap|array|plurals|integer|bool)/([A-Za-z0-9_.]+)")

errors: list[str] = []
warnings: list[str] = []

# ---------------------------------------------------------------- declarations
declared: dict[str, set[str]] = {}
packages: set[str] = set()
for path in SRC.rglob("*.kt"):
    text = path.read_text(encoding="utf-8")
    m = re.search(r"^package\s+([\w.]+)", text, re.M)
    if not m:
        errors.append(f"{path}: missing package declaration")
        continue
    pkg = m.group(1)
    packages.add(pkg)
    declared.setdefault(pkg, set()).update(DECL.findall(text))

    expected = ROOT / "app/src/main/java" / Path(pkg.replace(".", "/"))
    if path.parent != expected:
        warnings.append(f"{path.relative_to(ROOT)}: directory does not match package {pkg}")

# Generated at build time by AGP; nothing to resolve statically.
GENERATED = {f"{BASE_PKG}.BuildConfig", f"{BASE_PKG}.R"}

# ---------------------------------------------------------------- internal imports
for path in SRC.rglob("*.kt"):
    text = path.read_text(encoding="utf-8")
    for target in IMPORT.findall(text):
        if target in packages or target == BASE_PKG or target in GENERATED:
            continue
        pkg, _, symbol = target.rpartition(".")
        if symbol and symbol in declared.get(pkg, set()):
            continue
        errors.append(f"{path.relative_to(ROOT)}: unresolved import {target}")

# ---------------------------------------------------------------- R.* references
res_index: dict[str, set[str]] = {t: set() for t in
                                  ("string", "drawable", "color", "style", "xml", "mipmap",
                                   "array", "plurals", "integer", "bool")}
for path in RES.rglob("*"):
    if not path.is_file():
        continue
    folder = path.parent.name
    rtype = folder.split("-")[0]
    if rtype == "mipmap":
        rtype = "mipmap"
    if rtype in res_index:
        res_index[rtype].add(path.stem)
        if path.suffix == ".xml":
            text = path.read_text(encoding="utf-8", errors="ignore")
            res_index[rtype].update(re.findall(r'name="([A-Za-z0-9_.]+)"', text))
    elif folder.startswith("values"):
        # values/*.xml declares by tag: <color name="…">, <string name="…">, <style name="…">
        text = path.read_text(encoding="utf-8", errors="ignore")
        for tag, name in re.findall(r'<([a-z]+)\s+name="([A-Za-z0-9_.]+)"', text):
            if tag in res_index:
                res_index[tag].add(name)
                res_index[tag].add(name.replace(".", "_"))

for path in SRC.rglob("*.kt"):
    text = path.read_text(encoding="utf-8")
    for rtype, name in R_REF.findall(text):
        if name not in res_index.get(rtype, set()) and name.replace("_", ".") not in res_index.get(rtype, set()):
            errors.append(f"{path.relative_to(ROOT)}: R.{rtype}.{name} is not defined in res/")

for path in RES.rglob("*.xml"):
    if path.parent.name.startswith(("values", "xml")) and path.parent.name != "xml":
        continue
    text = path.read_text(encoding="utf-8", errors="ignore")
    for rtype, name in XML_REF.findall(text):
        if name.startswith("android:") or name.startswith("tools:"):
            continue
        if name not in res_index.get(rtype, set()):
            errors.append(f"{path.relative_to(RES.parent.parent)}: @{rtype}/{name} is not defined")

# ---------------------------------------------------------------- manifest sanity
manifest = (ROOT / "app/src/main/AndroidManifest.xml").read_text(encoding="utf-8")
for symbol in ("AfNewsApp", "MainActivity"):
    if symbol not in manifest:
        errors.append(f"AndroidManifest.xml: {symbol} is not referenced")

print(f"kotlin files : {len(list(SRC.rglob('*.kt')))}")
print(f"packages     : {len(packages)}")
print(f"declarations : {sum(len(v) for v in declared.values())}")
for w in warnings:
    print(f"WARN  {w}")
for e in errors:
    print(f"ERROR {e}")
print(("FAIL " + str(len(errors)) + " problem(s)") if errors else "OK — no unresolved references")
sys.exit(1 if errors else 0)

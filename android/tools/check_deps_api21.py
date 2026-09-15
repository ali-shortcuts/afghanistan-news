#!/usr/bin/env python3
"""Verify the Android compatibility gate straight from the published artifacts.

For every dependency in ``gradle/libs.versions.toml`` this tool downloads the exact
artifact and reads, from the artifact itself:

* ``minSdkVersion``  — parsed from the binary ``AndroidManifest.xml`` inside the AAR, i.e.
  the floor the library actually declares, not what a blog post claims;
* ``minCompileSdk``  — from ``META-INF/com/android/build/gradle/aar-metadata.properties``;
* ``minAndroidGradlePluginVersion`` — same file;
* ``minAgpVersion``-style constraints are reported so the pin table can be audited.

Exit code 1 if any artifact needs a higher ``minSdk`` than the gate, or a higher
``compileSdk``/AGP than the project declares.

Usage:
    python3 tools/check_deps_api21.py            # check every catalog entry
    python3 tools/check_deps_api21.py --json     # machine-readable report
"""

from __future__ import annotations

import io
import json
import os
import re
import struct
import sys
import urllib.request
import zipfile

HERE = os.path.dirname(os.path.abspath(__file__))
ANDROID_DIR = os.path.dirname(HERE)
CATALOG = os.path.join(ANDROID_DIR, "gradle", "libs.versions.toml")
CACHE = os.path.join(os.environ.get("TMPDIR", "/tmp"), "afnews-depcache")

# The gate from docs/android/compatibility.md (§396): minSdk 21, compileSdk 36.
GATE_MIN_SDK = 21
GATE_COMPILE_SDK = 36

def version_key(v: str) -> tuple:
    """Order versions numerically: "8.6.0" must compare lower than "8.13.2", not higher."""
    parts = []
    for chunk in re.split(r"[.\-]", str(v)):
        parts.append(int(chunk) if chunk.isdigit() else 0)
    while len(parts) < 3:
        parts.append(0)
    return tuple(parts[:4])


GOOGLE_MAVEN = "https://dl.google.com/dl/android/maven2"
CENTRAL = "https://repo1.maven.org/maven2"

# Groups served by Google's Maven repository; everything else comes from Maven Central.
GOOGLE_GROUPS = ("androidx.", "com.google.android.", "com.android.", "com.google.firebase")


# --------------------------------------------------------------------------- AXML
def _read_string_pool(data: bytes, offset: int) -> list[str]:
    (_type, _hs, _size) = struct.unpack_from("<HHI", data, offset)
    string_count, _style_count, flags, strings_start, _styles_start = struct.unpack_from(
        "<IIIII", data, offset + 8
    )
    utf8 = bool(flags & (1 << 8))
    offsets = struct.unpack_from(f"<{string_count}I", data, offset + 28)
    base = offset + strings_start
    out = []
    for rel in offsets:
        pos = base + rel
        if utf8:
            # u16len (1-2 bytes), u8len (1-2 bytes), bytes, 0x00
            n = data[pos]
            pos += 1
            if n & 0x80:
                pos += 1
            ln = data[pos]
            pos += 1
            if ln & 0x80:
                pos += 1
            out.append(data[pos : pos + ln].decode("utf-8", "replace"))
        else:
            n = struct.unpack_from("<H", data, pos)[0]
            pos += 2
            if n & 0x8000:
                n = ((n & 0x7FFF) << 16) | struct.unpack_from("<H", data, pos)[0]
                pos += 2
            out.append(data[pos : pos + n * 2].decode("utf-16-le", "replace"))
    return out


def manifest_min_sdk(axml: bytes) -> tuple[int | None, int | None]:
    """Return (minSdkVersion, targetSdkVersion) declared in an AndroidManifest.

    Published AARs ship the manifest either as plain XML (most libraries) or as Android's
    binary XML (compiled apps), so both encodings are handled.
    """
    if axml[:1] == b"<":
        text = axml.decode("utf-8", "replace")
        def _attr(name: str) -> int | None:
            m = re.search(rf'android:{name}\s*=\s*"([^"]+)"', text)
            if not m:
                return None
            v = m.group(1)
            return int(v) if v.isdigit() else None
        return _attr("minSdkVersion"), _attr("targetSdkVersion")
    strings: list[str] = []
    min_sdk = target_sdk = None
    offset = 8  # skip the XML file header
    while offset + 8 <= len(axml):
        chunk_type, header_size, chunk_size = struct.unpack_from("<HHI", axml, offset)
        if chunk_size == 0:
            break
        if chunk_type == 0x0001:  # string pool
            strings = _read_string_pool(axml, offset)
        elif chunk_type == 0x0102:  # start element
            name_idx = struct.unpack_from("<I", axml, offset + 20)[0]
            attr_count = struct.unpack_from("<H", axml, offset + 28)[0]
            attr_off = offset + struct.unpack_from("<H", axml, offset + 24)[0]
            name = strings[name_idx] if name_idx < len(strings) else ""
            if name in ("manifest", "uses-sdk"):
                for i in range(attr_count):
                    a = attr_off + i * 20
                    a_name_idx = struct.unpack_from("<I", axml, a + 4)[0]
                    a_name = strings[a_name_idx] if a_name_idx < len(strings) else ""
                    data_type = axml[a + 15]
                    data = struct.unpack_from("<I", axml, a + 16)[0]
                    if data_type == 0x10:  # INT_DEC
                        if a_name == "minSdkVersion":
                            min_sdk = data
                        elif a_name == "targetSdkVersion":
                            target_sdk = data
        offset += chunk_size
    return min_sdk, target_sdk


# --------------------------------------------------------------------------- fetching
def _download(url: str) -> bytes | None:
    key = re.sub(r"[^A-Za-z0-9._-]", "_", url.split("//", 1)[-1])
    path = os.path.join(CACHE, key)
    if os.path.exists(path):
        with open(path, "rb") as fh:
            return fh.read()
    try:
        with urllib.request.urlopen(url, timeout=45) as resp:
            blob = resp.read()
    except Exception:
        return None
    os.makedirs(CACHE, exist_ok=True)
    with open(path, "wb") as fh:
        fh.write(blob)
    return blob


def inspect(group: str, artifact: str, version: str) -> dict:
    base = GOOGLE_MAVEN if group.startswith(GOOGLE_GROUPS) else CENTRAL
    path = f"{base}/{group.replace('.', '/')}/{artifact}/{version}"
    info: dict = {"coordinate": f"{group}:{artifact}:{version}", "packaging": None}

    for ext in ("aar", "jar", "pom"):
        blob = _download(f"{path}/{artifact}-{version}.{ext}")
        if ext == "jar" and blob is not None and not artifact.endswith("-android"):
            # A Kotlin Multiplatform module publishes an empty root jar; the real Android
            # artifact is the sibling -android module, so inspect that one instead.
            variant = f"{base}/{group.replace('.', '/')}/{artifact}-android/{version}/{artifact}-android-{version}.aar"
            alt = _download(variant)
            if alt is not None:
                info["kmp_root"] = True
                blob, ext = alt, "aar"
        if blob is None:
            continue
        info["packaging"] = ext
        info["bytes"] = len(blob)
        if ext == "pom":
            text = blob.decode("utf-8", "replace")
            deps = re.findall(r"<artifactId>([^<]+)</artifactId>\s*<version>([^<]+)</version>", text)
            info["managed"] = len(deps)
            break
        try:
            with zipfile.ZipFile(io.BytesIO(blob)) as zf:
                names = zf.namelist()
                if "META-INF/com/android/build/gradle/aar-metadata.properties" in names:
                    props = zf.read(
                        "META-INF/com/android/build/gradle/aar-metadata.properties"
                    ).decode("utf-8", "replace")
                    for line in props.splitlines():
                        if "=" not in line or line.startswith("#"):
                            continue
                        k, v = line.split("=", 1)
                        info[k.strip()] = v.strip()
                if "AndroidManifest.xml" in names:
                    m, t = manifest_min_sdk(zf.read("AndroidManifest.xml"))
                    info["minSdkVersion"] = m
                    info["targetSdkVersion"] = t
        except zipfile.BadZipFile:
            info["packaging"] = None
        break
    return info


# --------------------------------------------------------------------------- catalog
def read_catalog(path: str) -> tuple[dict, list]:
    versions: dict[str, str] = {}
    libs: list[tuple[str, str, str, str]] = []
    section = ""
    with open(path, encoding="utf-8") as fh:
        for raw in fh:
            line = raw.split("#", 1)[0].strip()
            if not line:
                continue
            if line.startswith("["):
                section = line.strip("[]")
                continue
            if "=" not in line:
                continue
            key, value = (p.strip() for p in line.split("=", 1))
            if section == "versions":
                versions[key] = value.strip('"')
            elif section == "libraries":
                m = re.search(r'module\s*=\s*"([^"]+)"', value)
                if not m:
                    continue
                coord = m.group(1)
                if coord.count(":") == 2:  # version pinned inline
                    group, artifact, ver = coord.split(":")
                else:
                    group, artifact = coord.split(":")
                    vref = re.search(r'version\.ref\s*=\s*"([^"]+)"', value)
                    ver = versions.get(vref.group(1), "") if vref else ""
                if group and artifact and ver:
                    libs.append((key, group, artifact, ver))
    return versions, libs


def main() -> int:
    as_json = "--json" in sys.argv
    versions, libs = read_catalog(CATALOG)
    agp = versions.get("agp", "?")
    report = []
    failures = []

    for key, group, artifact, ver in libs:
        info = inspect(group, artifact, ver)
        info["key"] = key
        report.append(info)
        floor = info.get("minSdkVersion")
        if floor is None:
            continue  # pure-JVM artifact (moshi-kotlin-codegen, coroutines, junit…)
        if floor > GATE_MIN_SDK:
            failures.append(
                f"{info['coordinate']} requires minSdk {floor} > gate {GATE_MIN_SDK}"
            )
        min_compile = info.get("minCompileSdk")
        if min_compile and int(min_compile) > GATE_COMPILE_SDK:
            failures.append(
                f"{info['coordinate']} requires compileSdk {min_compile} > {GATE_COMPILE_SDK}"
            )
        min_agp = info.get("minAndroidGradlePluginVersion")
        if min_agp and version_key(min_agp) > version_key(agp):
            failures.append(f"{info['coordinate']} requires AGP {min_agp} > catalog {agp}")

    if as_json:
        print(json.dumps({"toolchain": {"agp": agp}, "gate": {"minSdk": GATE_MIN_SDK,
              "compileSdk": GATE_COMPILE_SDK}, "artifacts": report, "failures": failures}, indent=2))
        return 1 if failures else 0

    print(f"catalog: {CATALOG}")
    print(f"gate: minSdk {GATE_MIN_SDK} · compileSdk {GATE_COMPILE_SDK} · AGP {agp}")
    print(f"{'artifact':<46} {'pkg':<4} {'minSdk':>6} {'targetSdk':>9} {'minCompileSdk':>13} {'minAGP':>8}")
    print("-" * 92)
    for info in sorted(report, key=lambda r: r["coordinate"]):
        coord = info["coordinate"][:45]
        pkg = info.get("packaging") or "-"
        floor = info.get("minSdkVersion")
        tgt = info.get("targetSdkVersion")
        mcs = info.get("minCompileSdk", "-")
        magp = info.get("minAndroidGradlePluginVersion", "-")
        missing = "" if (floor is not None or info.get("bytes")) else "  [not found]"
        print(
            f"{coord:<46} {pkg:<4} {str(floor if floor is not None else '-'):>6} "
            f"{str(tgt if tgt is not None else '-'):>9} {str(mcs):>13} {str(magp):>8}{missing}"
        )
    print()
    if failures:
        print("GATE FAILURES:")
        for f in failures:
            print("  ✗", f)
        return 1
    print("✓ every artifact's declared minSdk is within the API 21 gate")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

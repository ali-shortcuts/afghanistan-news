#!/usr/bin/env bash
# Re-sign an APK with the project keystore.
#
# Why this exists: R8 and D8 need more memory than the 2 GB development container has, so a
# release variant is usually built on CI (7 GB runner) — but CI has no keystore, so its APK is
# debug-signed. This script turns that artifact into the shipping one: zipalign, then sign with
# android/afnews-release.jks, then verify what actually ended up in the file.
#
#   bash scripts/resign-apk.sh path/to/app-release.apk release-apk/afghanistan-news-1.0.0-signed.apk
#
# Signing identity resolution matches the Gradle build (env → android/keystore.properties), so an
# APK produced here upgrades over one produced by `./gradlew :app:assembleRelease`.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="${1:?usage: resign-apk.sh <input.apk> <output.apk>}"
OUT="${2:?usage: resign-apk.sh <input.apk> <output.apk>}"

BUILD_TOOLS="${BUILD_TOOLS:-$(ls -d /opt/android-sdk/build-tools/* 2>/dev/null | sort -V | tail -1)}"
ZIPALIGN="${BUILD_TOOLS}/zipalign"
APKSIGNER="${BUILD_TOOLS}/apksigner"
[ -x "${ZIPALIGN}" ] || { echo "zipalign not found in ${BUILD_TOOLS}" >&2; exit 1; }
[ -x "${APKSIGNER}" ] || { echo "apksigner not found in ${BUILD_TOOLS}" >&2; exit 1; }
[ -f "${SRC}" ] || { echo "no such APK: ${SRC}" >&2; exit 1; }

# Credentials: environment first, then android/keystore.properties, exactly like build.gradle.kts.
KEYSTORE="${ANDROID_KEYSTORE_FILE:-${ROOT}/android/afnews-release.jks}"
ALIAS="${ANDROID_KEY_ALIAS:-}"
STORE_PASS="${ANDROID_KEYSTORE_PASSWORD:-}"
KEY_PASS="${ANDROID_KEY_PASSWORD:-}"
PROPS="${ROOT}/android/keystore.properties"
if [ -f "${PROPS}" ]; then
  [ -n "${ALIAS}" ] || ALIAS="$(sed -n 's/^keyAlias=//p' "${PROPS}" | head -1)"
  [ -n "${STORE_PASS}" ] || STORE_PASS="$(sed -n 's/^storePassword=//p' "${PROPS}" | head -1)"
  [ -n "${KEY_PASS}" ] || KEY_PASS="$(sed -n 's/^keyPassword=//p' "${PROPS}" | head -1)"
  [ "${KEYSTORE}" != "${ROOT}/android/afnews-release.jks" ] || \
    KEYSTORE="$(sed -n 's/^storeFile=//p' "${PROPS}" | head -1 | sed "s|^|${ROOT}/android/|")"
fi
[ -f "${KEYSTORE}" ] || { echo "keystore missing: ${KEYSTORE} (and no ANDROID_KEYSTORE_FILE)" >&2; exit 1; }
[ -n "${ALIAS}" ] && [ -n "${STORE_PASS}" ] || { echo "no alias/password available" >&2; exit 1; }

echo "== 1/4 the input is a self-consistent APK"
"${APKSIGNER}" verify --print-certs "${SRC}" >/dev/null

echo "== 2/4 drop the old signature without touching any other entry"
# `zip -d` removes entries and leaves every remaining entry's data and compression method
# exactly as it was. Rebuilding the archive with unzip+zip would re-compress resources.arsc,
# and an APK whose resources.arsc is compressed does not install on Android 11+ at all.
mkdir -p "$(dirname "${OUT}")"
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT
UNSIGNED="${TMP}/unsigned.apk"
cp "${SRC}" "${UNSIGNED}"
( cd "${TMP}" && zip -q -d "${UNSIGNED}" "META-INF/*.RSA" "META-INF/*.SF" "META-INF/*.MF" "META-INF/*.EC" "META-INF/*.DSA" 2>/dev/null || true )

echo "== 3/4 sign with ${KEYSTORE##*/} (alias ${ALIAS})"
"${APKSIGNER}" sign \
  --ks "${KEYSTORE}" --ks-key-alias "${ALIAS}" \
  --ks-pass "pass:${STORE_PASS}" --key-pass "pass:${KEY_PASS:-${STORE_PASS}}" \
  --v1-signing-enabled true --v2-signing-enabled true \
  --out "${OUT}" "${UNSIGNED}"

echo "== 4/4 verify what actually ended up in the file"
"${APKSIGNER}" verify --print-certs "${OUT}" | sed 's/^/  /'
"${ZIPALIGN}" -c -p 4 "${OUT}" && echo "  zipalign: 4-byte aligned (page-aligned native libs) ✓"
if unzip -lv "${OUT}" | grep -q "resources.arsc" && unzip -lv "${OUT}" | grep "resources.arsc" | grep -qv "Stored"; then
  echo "  resources.arsc is COMPRESSED — Android 11+ will refuse to install this APK" >&2
  exit 1
fi
echo "  resources.arsc: stored uncompressed ✓"
echo "  size:  $(stat -c%s "${OUT}") bytes"
echo "  sha256: $(sha256sum "${OUT}" | cut -d' ' -f1)"

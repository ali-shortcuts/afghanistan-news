# Publishing an APK

How this project goes from source to something a phone can install, and what to keep safe.

## The artifact

| | |
|---|---|
| Package | `com.afghanistan.news` |
| Output | `android/app/build/outputs/apk/release/app-release.apk` |
| Architectures | `arm64-v8a` **and** `armeabi-v7a` in one APK (both `abiFilters`, no splits) |
| Android range | `minSdkVersion 21` (5.0) → `targetSdkVersion 36` (16) |
| Signing schemes | v1 (JAR) + v2 — v1 is what makes Android 5–6 install it at all |
| Size | ~11.7 MB release, ~17.4 MB debug, ~7 MB with R8 on a bigger machine |

One APK for 32-bit and 64-bit on purpose: a split-per-ABI release would halve the download but
double the "which file do I send my cousin" problem, and the Afghan market still has a large
32-bit share.

## Three ways to build it

```bash
# 1. Local, small machine (this container: 2 GB RAM) — signed, unshrunk
cd android && ./gradlew :app:assembleRelease -PminifyRelease=false

# 2. Local, 4 GB+ machine — signed and shrunk
cd android && ./gradlew :app:assembleRelease

# 3. CI — shrunk, verified and attached to a release (7 GB runner)
git tag -a v1.0.1 -m "…" && git push origin v1.0.1
```

R8 needs roughly 1.5 GB of heap for this dependency graph (Compose + Firebase + Room + Retrofit).
On a 2 GB box it is killed mid-run, so `-PminifyRelease=false` exists to produce a **working,
signed, installable** APK instead of an out-of-memory failure. The APK is identical in behaviour;
it is simply not shrunk. The Gradle log always says which path was taken:

```
release build: signed with the project keystore (afnews-release.jks)
release build: NO keystore found — falling back to the debug key (installable, not publishable)
```

## Signing

Credentials are looked up in this order, first hit wins:

1. Environment — `ANDROID_KEYSTORE_FILE`, `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`,
   `ANDROID_KEY_PASSWORD` (this is what CI uses, from repository secrets).
2. `android/keystore.properties` — a local release machine.
3. The debug key — so a fresh clone can still run `assembleRelease`.

`android/afnews-release.jks` and `android/keystore.properties` are **gitignored** and must stay
that way. Back them up somewhere you control:

```bash
cp android/afnews-release.jks android/keystore.properties ~/safe-place/
```

Losing them means you cannot install an update over an app that came from the signed APK — Android
rejects a package signed by a different key, and the only recovery is uninstall + reinstall, which
erases the user's saved articles. Losing them *and* the debug key is not fatal for sideloading, but
it ends any store presence.

To use the same keystore in CI, add four repository secrets
(**Settings → Secrets and variables → Actions**):

```bash
base64 -w0 android/afnews-release.jks      # → ANDROID_KEYSTORE_BASE64
# plus ANDROID_KEYSTORE_PASSWORD, ANDROID_KEY_ALIAS, ANDROID_KEY_PASSWORD
```

Without those secrets the workflow still succeeds, signs with the debug key, and prints a warning
that the artifact cannot be published to a store.

## What CI verifies before anything is attached

`.github/workflows/release-apk.yml` runs, in order: the API-21 dependency gate, the source
reference check, unit tests, the release build, and then a hard verification of the produced APK:

```
grep minSdkVersion:'21'                      # the compatibility floor
grep targetSdkVersion:'36'                   # the store target
grep native-code: 'arm64-v8a' 'armeabi-v7a'  # both architectures
unzip -l | grep lib/armeabi-v7a
unzip -l | grep lib/arm64-v8a
unzip -l | grep assets/feedpack/…v0.2.opml   # the OPML pack travels with the app
apksigner verify --print-certs               # it is actually signed
```

Only then is the APK uploaded as an artifact and attached to the tag's release — as
`afghanistan-news-<version>-minified.apk`, to stay distinguishable from a hand-built one.

## First run on a device

The app ships with a placeholder API address, so the first launch must be told where the news
comes from: **More → Settings → News server** (`بیشتر → تنظیمات → سرور خبر`), then *Test
connection* and *Save and reload*. Bring a server up with `scripts/preview.sh all`.

Accepted forms — `news.example.com`, `https://host/api`, `10.0.2.2:8080` (emulator),
`192.168.1.5:8080` (phone on the same Wi-Fi). Addresses without a scheme default to `https`,
except loopback and private/LAN addresses, which default to `http` because no certificate can be
trusted for them. `ServerUrl.normalize()` implements exactly these rules and is covered by ten
unit tests, including the trailing slash Retrofit needs to avoid mangling paths.

## Versioning checklist

1. Bump `versionCode` **and** `versionName` in `android/app/build.gradle.kts` (`versionCode` must
   increase, it is what Android uses to decide an update is newer).
2. Update the download table and the "verified" list in `README.md` if the numbers changed.
3. `git tag -a v<version> -m "…" && git push origin v<version>`.
4. Confirm the release page shows the artifact and the asset name matches the version.

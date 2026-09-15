# Android compatibility gate (API 21 / armeabi-v7a + arm64-v8a)

The client must install and run on Android 5.0 (API 21) and ship for both 32-bit and 64-bit
ARM. That is not a slogan in this repository: it is checked from the published artifacts, from
the compiler, and from the built APK.

## The rule

> **Pin the newest release whose own `AndroidManifest.xml` still declares `minSdkVersion 21`.**

Nothing is pinned because it is old, and nothing is upgraded because it is new. Every version in
`gradle/libs.versions.toml` is the ceiling of its line, and the ceiling is discovered by reading
the artifact — `python3 tools/check_deps_api21.py` downloads each AAR and prints the floor it
declares, failing the run if any dependency asks for more than API 21.

An earlier revision of this file claimed "Room 2.7.x requires minSdk 23" and consequently pinned
Room 2.6.1, WorkManager 2.9.1 and Paging 3.3.2. That claim was **wrong**, and it cost a year of
library updates. Measured floors:

| Library | Version | Declared minSdk | Next line up | Why we stop here |
|---|---|---|---|---|
| Room | **2.7.2** | 21 | 2.8.5 = **23** | paging/coroutines support without losing Android 5 |
| WorkManager | **2.10.5** | 21 | 2.11.2 = **23** | periodic sync keeps working on API 21 |
| Paging | **3.3.6** | 21 | 3.5.1 = **23** | PagingSource DAOs stay available |
| Compose (BOM) | **2025.11.00** (ui 1.9.4, material3 1.4.0) | 21 | ui 1.10.0 = **23** | the whole Compose line moves to 23 at 1.10 |
| core-ktx | 1.16.0 | 21 | 1.19.0 = 23 | |
| activity-compose | 1.10.1 | 21 | 1.13.0 = 23 | |
| lifecycle | 2.9.4 | 21 | 2.11.0 = 23 | |
| navigation-compose | 2.9.5 | 21 | 2.10.1 = 24 | |
| datastore-preferences | 1.1.7 | 19 | 1.2.1 = 23 | |
| androidx.hilt | 1.2.0 | 19 | 1.3.0 = 23 | Hilt's ViewModel/Work integration |
| firebase-bom | 33.16.0 (messaging 24.1.2) | 21 | BOM 34 → messaging 25 = 23 | push only, no analytics SDK |
| Coil | 3.3.0 | 21 | — | Coil 2 is end-of-line; 3 still supports 21 |
| material-icons-core | 1.7.8 | 21 | extended = ~30k methods | only 3 icons were missing; they ship as drawables |
| mockk (test) | 1.14.0 | 21 | 1.14.7 = 26 | unit tests run on the JVM, but the gate is applied uniformly |

## Toolchain

| Component | Version | Note |
|---|---|---|
| JDK | 21 | AGP 8.13 requires 17+; CI and this environment use 21 |
| Gradle | 8.14.5 | wrapper committed (`gradlew`, `gradlew.bat`, `gradle-wrapper.jar`) |
| Android Gradle Plugin | 8.13.2 | last line that pairs with Kotlin 2.2.x |
| Kotlin / Compose compiler | 2.2.21 | the compiler generation Compose 1.9 was built against |
| KSP | 2.2.21-2.0.5 | replaced kapt for Room, Hilt and Moshi |
| compileSdk / targetSdk | **36** | build-tools 36.0.0 pinned |
| minSdk | **21** | the gate |
| NDK | not used | no native code; `abiFilters` alone produces both ABIs |

Hilt 2.60+ and AGP 9.x are deliberately not used: Hilt 2.60 **requires** AGP 9, and AGP 9 pulls a
toolchain generation whose Compose line starts at minSdk 23. The gate decides the toolchain, not
the other way round.

## How the gate is enforced

1. **Artifact level** — `tools/check_deps_api21.py` fails if any dependency declares
   `minSdkVersion > 21`, `minCompileSdk > 36`, or a newer minimum AGP than the catalog uses.
   Run in CI and by `scripts/acceptance.sh`.
2. **Source level** — `tools/check_sources.py` resolves every internal import, `R.*` reference
   and manifest symbol; it catches reference rot without needing the SDK.
3. **Compiler level** — `:app:lintDebug` fails the build on `NewApi`. This is not theoretical:
   the first real lint run found `Configuration.locales[0]` in `PlatformCompat.kt`, which is
   **API 24+** and would have crashed on Android 5/6. It now branches on
   `Build.VERSION_CODES.N` and reads the deprecated `configuration.locale` on API 21-23.
4. **APK level** — CI (and this workspace) dumps `aapt2 dump badging` of the built APK and
   asserts `minSdkVersion:'21'`, `targetSdkVersion:'36'`,
   `native-code: 'arm64-v8a' 'armeabi-v7a'` and the bundled feed pack.

## What the built APK actually reports

```
package: name='com.afghanistan.news.debug' versionCode='1' versionName='1.0.0-debug'
compileSdkVersion='36' compileSdkVersionCodename='16'
minSdkVersion:'21'
targetSdkVersion:'36'
application-label:'خبر افغانستان'
application-label-fa:'خبر افغانستان'
application-label-ps:'افغانستان خبرونه'
launchable-activity: name='com.afghanistan.news.MainActivity'
native-code: 'arm64-v8a' 'armeabi-v7a'
uses-permission: android.permission.INTERNET
uses-permission: android.permission.ACCESS_NETWORK_STATE
uses-permission: android.permission.POST_NOTIFICATIONS
uses-permission: android.permission.RECEIVE_BOOT_COMPLETED
uses-permission: com.google.android.c2dm.permission.RECEIVE
uses-permission: android.permission.FOREGROUND_SERVICE
assets/feedpack/afghanistan-global-news-master-v0.2.opml   196 590 bytes
app-debug.apk                                               18 MB
```

## Footguns found by building (each is a rule now)

* A version catalog is **TOML**: `//` is not a comment. The catalog had been using `//` since it
  was written and had never once been parsed, because the module had never been compiled.
* `kapt` is gone; KSP runs Room, Hilt and Moshi. `kotlinOptions` was removed in Kotlin 2.2, so
  the build uses the typed `kotlin { compilerOptions { … } }` DSL.
* WorkManager 2.10 fails the manifest merge if the app also declares
  `RescheduleReceiver` — WorkManager gates its receivers with
  `@bool/enable_system_alarm_service_default`, which is already `true` on API 21-22.
* Room 2.7 generates `LimitOffsetPagingSource` in `room-paging`; without that artifact on the
  classpath a `PagingSource` DAO fails at KSP time.
* material3 1.4 no longer brings `material-icons-core`, and `Icons.Filled.Bookmark` /
  `BookmarkBorder` are not in the core set at all — they are bundled as vector drawables.
  `ArrowBack` is deprecated in favour of the auto-mirrored variant, which is the correct choice
  for a right-to-left app.
* Back-up rules: every `<exclude>` must live inside an `<include>` path. Only the Room database
  (the saved-articles list) is backed up; the push token and preferences stay on the device.
* App Links want their own intent filter: mixing `https` and a custom scheme in one filter makes
  domain verification ambiguous.

## Reproducing

```bash
cd android
python3 tools/check_deps_api21.py           # artifact-level gate
python3 tools/check_sources.py              # source-level gate
./gradlew :app:assembleDebug :app:testDebugUnitTest :app:lintDebug
/opt/android-sdk/build-tools/36.0.0/aapt2 dump badging app/build/outputs/apk/debug/app-debug.apk
```

The build is verified on a 2 vCPU / 2 GB container: `org.gradle.jvmargs=-Xmx700m`,
`kotlin.compiler.execution.strategy=in-process`, `org.gradle.workers.max=1`. Everything fits
because the dex no longer carries `material-icons-extended` or the Firebase Analytics SDK.

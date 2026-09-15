# Afghanistan News — Android client

Kotlin · Jetpack Compose · Room · Hilt · Retrofit/OkHttp · Paging 3 · DataStore · WorkManager · Coil · FCM

The reader app for the Afghanistan News platform. Offline-first: Room is the local source of
truth, every article shows its source attribution, nothing in v1 requires an account.

```
app/src/main/java/com/afghanistan/news
├── core/    model · network (Retrofit + Moshi) · database (Room) · datastore · feed pack · platform compat
├── data/    repositories, paging, background sync workers
├── di/      Hilt modules (network, database, app bindings)
├── push/    FCM service + anonymous topic catalog
└── ui/      theme, shared components, navigation, screens + view models
```

## Compatibility gate — API 21 (Android 5.0)

| Item | Value |
|---|---|
| minSdk | 21 (Android 5.0) |
| targetSdk / compileSdk | 35 |
| ABIs | `armeabi-v7a`, `arm64-v8a` (pure Kotlin, no NDK) |
| Java | 17 with core-library desugaring (`java.time` on API 21) |
| Room | 2.6.1 — 2.7.x raised minSdk to 23 |
| WorkManager | 2.9.1 — 2.10.x raised minSdk to 23 |
| Paging | 3.3.2 |
| Compose | BOM 2024.10.01, Material 3 |
| Activity | `AppCompatActivity` + `Theme.AppCompat.DayNight.NoActionBar` |

Every ceiling is pinned in `gradle/libs.versions.toml` with the reason attached. No dynamic
colour, no foreground service, no exact alarms, no native code.

## Build

```bash
# 1. JDK 17 + Android SDK 35 (this workspace has neither, so builds run in CI)
sdkmanager "platforms;android-35" "build-tools;35.0.0"

# 2. (optional) FCM: drop app/google-services.json in — the plugin stays off without it
# 3. local.properties: apiBaseUrl=http://10.0.2.2:8080/   (emulator → host machine)

./gradlew :app:assembleDebug          # → app/build/outputs/apk/debug/
./gradlew :app:testDebugUnitTest      # JVM tests: DTO mapping, refresh policy
./gradlew :app:lintDebug              # the API-21 gate (fails on NewApi violations)
./gradlew :app:assembleRelease        # R8 full mode + resource shrinking
```

The bundled OPML feed pack is copied from `../feedpacks/` into `src/main/assets/feedpack/` by
the `syncFeedPack` task on every build; the committed copy is the fallback.

## Screen contract

Home · Afghanistan · World · Saved · More, plus province, category, search, article,
notifications, settings and about. Every screen implements Loading / Content / Empty / Error
through `ui/components/StateViews.kt` — a screen that cannot render an empty state is
unfinished. Deep links (`/a/{id}`, `/province/{id}`, search) are declared in the manifest and
resolved by `ui/navigation/Destinations.kt`.

## Background cadence

| Worker | Interval | Purpose |
|---|---|---|
| `RefreshWorker` | 30 min (+ expedited on push) | refresh Home / Afghanistan / World, prune cache |
| `PushSyncWorker` | 24 h | re-register the anonymous device token + topics |
| `CacheCleanupWorker` | 12 h | enforce the storage budget, expire offline items |

## Privacy posture

* No account, no login, no email collection.
* Push topics are anonymous device-level flags, removable from Settings.
* Bookmarks live only on-device.
* The app never republishes publisher full text — it links out through Chrome Custom Tabs.

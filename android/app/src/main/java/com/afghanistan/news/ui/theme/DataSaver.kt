package com.afghanistan.news.ui.theme

import androidx.compose.runtime.staticCompositionLocalOf

/**
 * Data-saver flag provided at the app root (Settings → ذخیرهٔ داده).
 * When true, list and reader images are skipped so a metered connection
 * only pays for text (v1.3). Default false = images on.
 */
val LocalDataSaver = staticCompositionLocalOf { false }

package com.afghanistan.news.ui.theme

import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.platform.LocalLayoutDirection
import com.afghanistan.news.core.model.Language

/**
 * Design tokens (Architecture §143-§150). Spacing 4/8/12/16/24/32, corners 8/12/16/20,
 * minimum touch target 48dp, headline-first typography with generous line height because
 * Persian/Arabic script needs vertical room.
 */
object AfNewsSpacing {
    val xs = 4.dp
    val sm = 8.dp
    val md = 12.dp
    val lg = 16.dp
    val xl = 24.dp
    val xxl = 32.dp
    val minTouchTarget = 48.dp
}

object AfNewsShapes {
    val small = 8.dp
    val medium = 12.dp
    val large = 16.dp
    val extraLarge = 20.dp
}

private val Teal = Color(0xFF0F766E)
private val TealDark = Color(0xFF2DD4BF)
private val Ink = Color(0xFF12161C)
private val Paper = Color(0xFFF6F7F9)
private val Alert = Color(0xFFB42318)

private val LightColors = lightColorScheme(
    primary = Teal,
    onPrimary = Color.White,
    primaryContainer = Color(0xFFE6F4F2),
    onPrimaryContainer = Color(0xFF0B5C56),
    secondary = Color(0xFF475467),
    background = Paper,
    onBackground = Ink,
    surface = Color.White,
    onSurface = Ink,
    surfaceVariant = Color(0xFFEEF1F4),
    onSurfaceVariant = Color(0xFF5B6472),
    error = Alert,
    outline = Color(0xFFD9DEE5),
)

private val DarkColors = darkColorScheme(
    primary = TealDark,
    onPrimary = Color(0xFF06221F),
    primaryContainer = Color(0xFF10312D),
    onPrimaryContainer = Color(0xFF9FE3DA),
    secondary = Color(0xFF98A2B3),
    background = Color(0xFF0F1319),
    onBackground = Color(0xFFE7ECF2),
    surface = Color(0xFF161B22),
    onSurface = Color(0xFFE7ECF2),
    surfaceVariant = Color(0xFF1D232C),
    onSurfaceVariant = Color(0xFFAFBAC8),
    error = Color(0xFFF97066),
    outline = Color(0xFF2A323C),
)

/** Persian/Arabic first font stack; the platform falls back per-glyph on API 21 devices. */
private val NewsFontFamily = FontFamily.SansSerif

val AfNewsTypography = Typography(
    headlineLarge = TextStyle(fontFamily = NewsFontFamily, fontWeight = FontWeight.Bold, fontSize = 24.sp, lineHeight = 38.sp),
    headlineMedium = TextStyle(fontFamily = NewsFontFamily, fontWeight = FontWeight.Bold, fontSize = 20.sp, lineHeight = 34.sp),
    titleLarge = TextStyle(fontFamily = NewsFontFamily, fontWeight = FontWeight.Bold, fontSize = 18.sp, lineHeight = 32.sp),
    titleMedium = TextStyle(fontFamily = NewsFontFamily, fontWeight = FontWeight.SemiBold, fontSize = 16.sp, lineHeight = 28.sp),
    bodyLarge = TextStyle(fontFamily = NewsFontFamily, fontSize = 15.sp, lineHeight = 28.sp),
    bodyMedium = TextStyle(fontFamily = NewsFontFamily, fontSize = 13.5.sp, lineHeight = 26.sp),
    bodySmall = TextStyle(fontFamily = NewsFontFamily, fontSize = 12.sp, lineHeight = 22.sp),
    labelSmall = TextStyle(fontFamily = NewsFontFamily, fontWeight = FontWeight.SemiBold, fontSize = 11.sp, lineHeight = 18.sp),
)

/** True when the device locale should mirror the layout for Dari/Pashto (§150). */
val LocalAfNewsLanguage = staticCompositionLocalOf { Language.DARI }

@Composable
fun AfNewsTheme(
    themeMode: String = "system",
    languageTag: String = "fa",
    content: @Composable () -> Unit,
) {
    val dark = when (themeMode) {
        "dark" -> true
        "light" -> false
        else -> isSystemInDarkTheme()
    }
    val language = Language.from(languageTag)
    val layoutDirection = when (language) {
        Language.ENGLISH -> LayoutDirection.Ltr
        else -> LayoutDirection.Rtl
    }
    val configuration = LocalConfiguration.current
    // Dynamic color exists only on API 31+; deliberately unused so the brand is identical
    // on Android 5.0 and Android 16 (§396).
    val scheme = if (dark) DarkColors else LightColors
    val context = LocalContext.current

    CompositionLocalProvider(
        LocalAfNewsLanguage provides language,
        LocalLayoutDirection provides layoutDirection,
    ) {
        MaterialTheme(
            colorScheme = scheme,
            typography = AfNewsTypography,
            shapes = androidx.compose.material3.Shapes(
                small = androidx.compose.foundation.shape.RoundedCornerShape(AfNewsShapes.small),
                medium = androidx.compose.foundation.shape.RoundedCornerShape(AfNewsShapes.medium),
                large = androidx.compose.foundation.shape.RoundedCornerShape(AfNewsShapes.large),
                extraLarge = androidx.compose.foundation.shape.RoundedCornerShape(AfNewsShapes.extraLarge),
            ),
            content = content,
        )
    }
    // `context` is intentionally unused today: platform-font overrides for API 21 devices
    // will need it, and the parameter keeps every call site unchanged.
}

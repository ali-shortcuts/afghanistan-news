package com.afghanistan.news.ui.theme

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
 *
 * v1.2: the palette moved from a single muted teal to a vivid, high-contrast brand —
 * emerald primary, amber secondary, azure tertiary — plus a deterministic color per
 * news category so every menu, chip and badge is recognisable at a glance.
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

private val Emerald = Color(0xFF059669)
private val EmeraldDark = Color(0xFF34D399)
private val Amber = Color(0xFFF59E0B)
private val AmberDark = Color(0xFFFBBF24)
private val Azure = Color(0xFF2563EB)
private val AzureDark = Color(0xFF60A5FA)
private val Ink = Color(0xFF101720)
private val Paper = Color(0xFFF7F9FB)
private val Alert = Color(0xFFDC2626)

private val LightColors = lightColorScheme(
    primary = Emerald,
    onPrimary = Color.White,
    primaryContainer = Color(0xFFD1FAE5),
    onPrimaryContainer = Color(0xFF065F46),
    secondary = Amber,
    onSecondary = Color(0xFF3B2A00),
    secondaryContainer = Color(0xFFFEF3C7),
    onSecondaryContainer = Color(0xFF92400E),
    tertiary = Azure,
    onTertiary = Color.White,
    tertiaryContainer = Color(0xFFDBEAFE),
    onTertiaryContainer = Color(0xFF1E40AF),
    background = Paper,
    onBackground = Ink,
    surface = Color.White,
    onSurface = Ink,
    surfaceVariant = Color(0xFFEDF2F6),
    onSurfaceVariant = Color(0xFF57646F),
    error = Alert,
    onError = Color.White,
    errorContainer = Color(0xFFFEE2E2),
    onErrorContainer = Color(0xFF991B1B),
    outline = Color(0xFFD6DEE6),
)

private val DarkColors = darkColorScheme(
    primary = EmeraldDark,
    onPrimary = Color(0xFF04241A),
    primaryContainer = Color(0xFF0B3B2D),
    onPrimaryContainer = Color(0xFFA7F3D0),
    secondary = AmberDark,
    onSecondary = Color(0xFF2C1E00),
    secondaryContainer = Color(0xFF4A3403),
    onSecondaryContainer = Color(0xFFFDE68A),
    tertiary = AzureDark,
    onTertiary = Color(0xFF0A1B3D),
    tertiaryContainer = Color(0xFF1E3A8A),
    onTertiaryContainer = Color(0xFFBFDBFE),
    background = Color(0xFF0E1319),
    onBackground = Color(0xFFE7ECF2),
    surface = Color(0xFF151B23),
    onSurface = Color(0xFFE7ECF2),
    surfaceVariant = Color(0xFF1D2530),
    onSurfaceVariant = Color(0xFFAFBAC8),
    error = Color(0xFFF87171),
    onError = Color(0xFF2C0B0B),
    errorContainer = Color(0xFF7F1D1D),
    onErrorContainer = Color(0xFFFECACA),
    outline = Color(0xFF2A3440),
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

/**
 * Deterministic accent color per canonical category id (§29.5 keys). The hue travels with
 * the category everywhere — chips, badges, category tiles and feed top bars — so readers
 * recognise a topic by color before they read its label.
 */
object AfNewsCategoryColors {
    private val map = mapOf(
        "afghanistan" to Color(0xFFEA580C),
        "breaking" to Color(0xFFDC2626),
        "politics" to Color(0xFF7C3AED),
        "economy" to Color(0xFF059669),
        "finance" to Color(0xFF0D9488),
        "security" to Color(0xFFB91C1C),
        "society" to Color(0xFFDB2777),
        "provincial" to Color(0xFF65A30D),
        "jobs" to Color(0xFF2563EB),
        "opportunities" to Color(0xFF16A34A),
        "tender" to Color(0xFFA16207),
        "migration" to Color(0xFF0891B2),
        "health" to Color(0xFFE11D48),
        "education" to Color(0xFF4F46E5),
        "humanitarian" to Color(0xFFF59E0B),
        "world" to Color(0xFF0284C7),
        "regional" to Color(0xFF8B5CF6),
        "technology" to Color(0xFF0E7490),
        "ai" to Color(0xFF6366F1),
        "crypto" to Color(0xFFF97316),
        "science" to Color(0xFF9333EA),
        "climate" to Color(0xFF15803D),
        "disasters" to Color(0xFF991B1B),
        "sports" to Color(0xFF4D7C0F),
        "cricket" to Color(0xFF0F766E),
        "culture" to Color(0xFFC026D3),
        "media" to Color(0xFF334155),
        "official" to Color(0xFF475569),
        "energy" to Color(0xFFD97706),
        "agriculture" to Color(0xFF3F6212),
    )

    /** Accent for a category id; unknown ids fall back to the emerald brand color. */
    fun of(categoryId: String?): Color =
        categoryId?.let { map[it] } ?: Emerald

    /** True when the accent is bright enough to need dark text on it (readability §143). */
    fun onColor(color: Color): Color =
        if (color == Color(0xFFF59E0B) || color == Color(0xFFF97316) || color == Color(0xFFD97706)) {
            Color(0xFF2C1E00)
        } else {
            Color.White
        }
}

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

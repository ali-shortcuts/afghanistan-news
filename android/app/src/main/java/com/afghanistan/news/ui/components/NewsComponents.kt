package com.afghanistan.news.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import coil3.compose.AsyncImage
import com.afghanistan.news.core.model.Article
import com.afghanistan.news.ui.theme.AfNewsShapes
import com.afghanistan.news.ui.theme.AfNewsSpacing

/**
 * Shared news UI atoms. Every card carries attribution; the design system forbids an
 * article visual without a source label (Architecture §8, §143).
 */
@Composable
fun BreakingStrip(article: Article, coverageCount: Int, onClick: () -> Unit) {
    Surface(
        color = MaterialTheme.colorScheme.error,
        shape = RoundedCornerShape(AfNewsShapes.large),
        modifier = Modifier
            .fillMaxWidth()
            .padding(bottom = AfNewsSpacing.md)
            .clickable(onClick = onClick),
    ) {
        Column(Modifier.padding(AfNewsSpacing.lg)) {
            Text("خبر فوری", style = MaterialTheme.typography.labelSmall, color = Color.White.copy(alpha = 0.92f))
            Text(
                article.title,
                style = MaterialTheme.typography.titleLarge,
                color = Color.White,
                modifier = Modifier.padding(top = AfNewsSpacing.xs),
            )
            Text(
                "${article.source.name} · ${coverageCount} منبع",
                style = MaterialTheme.typography.bodySmall,
                color = Color.White.copy(alpha = 0.9f),
                modifier = Modifier.padding(top = AfNewsSpacing.xs),
            )
        }
    }
}

@Composable
fun ArticleCard(
    article: Article,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    compact: Boolean = false,
) {
    Card(
        modifier = modifier
            .fillMaxWidth()
            .padding(bottom = AfNewsSpacing.sm)
            .clickable(onClick = onClick),
        shape = RoundedCornerShape(AfNewsShapes.medium),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surface),
        elevation = CardDefaults.cardElevation(defaultElevation = 1.dp),
    ) {
        Column {
            if (!compact && !article.imageUrl.isNullOrBlank()) {
                AsyncImage(
                    model = article.imageUrl,
                    contentDescription = null,
                    contentScale = ContentScale.Crop,
                    modifier = Modifier
                        .fillMaxWidth()
                        .aspectRatio(16f / 9f)
                        .clip(RoundedCornerShape(topStart = AfNewsShapes.medium, topEnd = AfNewsShapes.medium)),
                )
            }
            Column(Modifier.padding(AfNewsSpacing.md)) {
                Text(
                    article.title,
                    style = if (compact) MaterialTheme.typography.titleMedium else MaterialTheme.typography.titleLarge,
                    maxLines = if (compact) 3 else 4,
                    overflow = TextOverflow.Ellipsis,
                )
                if (!compact && !article.summary.isNullOrBlank()) {
                    Text(
                        article.summary,
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 3,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.padding(top = AfNewsSpacing.xs),
                    )
                }
                AttributionRow(article)
            }
        }
    }
}

/** Always-visible source attribution: name, transparency label and breaking flag. */
@Composable
fun AttributionRow(article: Article) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = AfNewsSpacing.sm),
        horizontalArrangement = Arrangement.spacedBy(AfNewsSpacing.sm),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Badge(article.source.name, primary = true)
        article.category?.let { Badge(it.localized("fa")) }
        article.province?.let { Badge(it.name) }
        if (article.isBreaking) Badge("فوری", alert = true)
    }
}

@Composable
fun Badge(text: String, primary: Boolean = false, alert: Boolean = false) {
    val background = when {
        alert -> MaterialTheme.colorScheme.error
        primary -> MaterialTheme.colorScheme.primaryContainer
        else -> MaterialTheme.colorScheme.surfaceVariant
    }
    val foreground = when {
        alert -> Color.White
        primary -> MaterialTheme.colorScheme.onPrimaryContainer
        else -> MaterialTheme.colorScheme.onSurfaceVariant
    }
    Surface(color = background, shape = RoundedCornerShape(percent = 50)) {
        Text(
            text,
            style = MaterialTheme.typography.labelSmall,
            color = foreground,
            maxLines = 1,
            modifier = Modifier.padding(horizontal = AfNewsSpacing.sm, vertical = 2.dp),
        )
    }
}

@Composable
fun SectionHeader(title: String, onSeeAll: (() -> Unit)? = null) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = AfNewsSpacing.lg, bottom = AfNewsSpacing.sm),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(title, style = MaterialTheme.typography.titleMedium)
        if (onSeeAll != null) {
            Text(
                "همه ←",
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.primary,
                modifier = Modifier
                    .clickable(onClick = onSeeAll)
                    .padding(vertical = AfNewsSpacing.xs),
            )
        }
    }
}

/** Loading placeholder used by every screen while Room + network settle (§70). */
@Composable
fun SkeletonCard() {
    Column(Modifier.padding(vertical = AfNewsSpacing.sm)) {
        Box(
            Modifier
                .fillMaxWidth()
                .height(140.dp)
                .clip(RoundedCornerShape(AfNewsShapes.medium))
                .background(MaterialTheme.colorScheme.surfaceVariant),
        )
        Box(
            Modifier
                .padding(top = AfNewsSpacing.sm)
                .fillMaxWidth(0.85f)
                .height(14.dp)
                .clip(RoundedCornerShape(AfNewsShapes.small))
                .background(MaterialTheme.colorScheme.surfaceVariant),
        )
        Box(
            Modifier
                .padding(top = AfNewsSpacing.xs)
                .fillMaxWidth(0.55f)
                .height(12.dp)
                .clip(RoundedCornerShape(AfNewsShapes.small))
                .background(MaterialTheme.colorScheme.surfaceVariant),
        )
    }
}

@Composable
fun SplashPlaceholder() {
    Box(Modifier.size(1.dp))
}

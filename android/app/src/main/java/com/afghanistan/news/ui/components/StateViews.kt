package com.afghanistan.news.ui.components

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.afghanistan.news.ui.theme.AfNewsSpacing

/**
 * The four required screen states (§70). Using these guarantees that no screen silently
 * shows a blank page when the network drops or a list is empty.
 */
@Composable
fun LoadingState(modifier: Modifier = Modifier, rows: Int = 3) {
    Column(modifier.fillMaxSize().padding(AfNewsSpacing.md)) {
        repeat(rows) { SkeletonCard() }
    }
}

@Composable
fun EmptyState(
    title: String = "محتوایی موجود نیست",
    detail: String = "به‌زودی خبرهای تازه در این بخش منتشر می‌شود.",
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier.fillMaxSize().padding(AfNewsSpacing.xl),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Text("🗞", style = MaterialTheme.typography.headlineLarge)
        Text(title, style = MaterialTheme.typography.titleMedium, modifier = Modifier.padding(top = AfNewsSpacing.md))
        Text(
            detail,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center,
            modifier = Modifier.padding(top = AfNewsSpacing.xs).widthIn(max = 320.dp),
        )
    }
}

@Composable
fun ErrorState(
    message: String,
    onRetry: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier.fillMaxSize().padding(AfNewsSpacing.xl),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Text("⚠", style = MaterialTheme.typography.headlineLarge)
        Text(
            message,
            style = MaterialTheme.typography.bodyLarge,
            textAlign = TextAlign.Center,
            modifier = Modifier.padding(vertical = AfNewsSpacing.md).widthIn(max = 320.dp),
        )
        if (onRetry != null) {
            Button(onClick = onRetry) { Text("تلاش دوباره") }
        }
    }
}

/** Offline banner shown when the list came from cache rather than the network. */
@Composable
fun OfflineBanner(modifier: Modifier = Modifier) {
    Text(
        "آفلاین — نمایش آخرین نسخهٔ ذخیره‌شده",
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        textAlign = TextAlign.Center,
        modifier = modifier
            .fillMaxSize()
            .padding(AfNewsSpacing.sm),
    )
}

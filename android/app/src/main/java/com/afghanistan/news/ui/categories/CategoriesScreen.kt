package com.afghanistan.news.ui.categories

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import com.afghanistan.news.core.datastore.SettingsStore
import com.afghanistan.news.ui.navigation.CategoryCatalog
import com.afghanistan.news.ui.theme.AfNewsCategoryColors
import com.afghanistan.news.ui.theme.AfNewsShapes
import com.afghanistan.news.ui.theme.AfNewsSpacing
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.stateIn
import javax.inject.Inject

/** Exposes the active language so the catalog labels follow the reader's choice. */
@HiltViewModel
class CategoriesViewModel @Inject constructor(
    settings: SettingsStore,
) : ViewModel() {
    val languageTag: StateFlow<String> =
        settings.language.stateIn(viewModelScope, SharingStarted.Eagerly, "fa")
}

/**
 * Colorful hub of every news type: each tile opens that category's own dedicated feed
 * screen, so topics never blur into one mixed list.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CategoriesScreen(
    onOpenCategory: (String) -> Unit,
    viewModel: CategoriesViewModel = hiltViewModel(),
) {
    val languageTag by viewModel.languageTag.collectAsStateWithLifecycle()

    Scaffold(
        topBar = { TopAppBar(title = { Text("دسته‌بندی‌ها") }) },
    ) { padding ->
        Column(
            Modifier
                .fillMaxSize()
                .padding(padding),
        ) {
            // Gradient banner — the visual anchor of the hub.
            Box(
                Modifier
                    .fillMaxWidth()
                    .padding(horizontal = AfNewsSpacing.lg)
                    .height(88.dp)
                    .background(
                        brush = Brush.linearGradient(
                            listOf(
                                MaterialTheme.colorScheme.primary,
                                MaterialTheme.colorScheme.tertiary,
                                MaterialTheme.colorScheme.secondary,
                            ),
                        ),
                        shape = RoundedCornerShape(AfNewsShapes.extraLarge),
                    ),
                contentAlignment = Alignment.Center,
            ) {
                Column(horizontalAlignment = Alignment.CenterHorizontally) {
                    Text(
                        "هر نوع خبر، منوی خودش",
                        style = MaterialTheme.typography.titleLarge,
                        color = Color.White,
                    )
                    Text(
                        "یک دسته را انتخاب کنید",
                        style = MaterialTheme.typography.bodySmall,
                        color = Color.White.copy(alpha = 0.9f),
                    )
                }
            }
            Spacer(Modifier.height(AfNewsSpacing.md))
            LazyVerticalGrid(
                columns = GridCells.Fixed(2),
                contentPadding = PaddingValues(
                    start = AfNewsSpacing.lg,
                    end = AfNewsSpacing.lg,
                    bottom = AfNewsSpacing.xl,
                ),
                horizontalArrangement = Arrangement.spacedBy(AfNewsSpacing.md),
                verticalArrangement = Arrangement.spacedBy(AfNewsSpacing.md),
                modifier = Modifier.fillMaxSize(),
            ) {
                items(CategoryCatalog.items, key = { it.id }) { item ->
                    val accent = AfNewsCategoryColors.of(item.id)
                    Surface(
                        shape = RoundedCornerShape(AfNewsShapes.large),
                        color = MaterialTheme.colorScheme.surface,
                        tonalElevation = 1.dp,
                        modifier = Modifier
                            .fillMaxWidth()
                            .clickable { onOpenCategory(item.id) },
                    ) {
                        Column(
                            Modifier
                                .fillMaxWidth()
                                .padding(AfNewsSpacing.md),
                            horizontalAlignment = Alignment.CenterHorizontally,
                        ) {
                            Box(
                                Modifier
                                    .size(44.dp)
                                    .background(accent, RoundedCornerShape(AfNewsShapes.medium)),
                                contentAlignment = Alignment.Center,
                            ) {
                                Text(item.icon, style = MaterialTheme.typography.titleLarge)
                            }
                            Text(
                                CategoryCatalog.localized(item.id, languageTag),
                                style = MaterialTheme.typography.titleMedium,
                                textAlign = TextAlign.Center,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                                modifier = Modifier.padding(top = AfNewsSpacing.sm),
                            )
                            Text(
                                "خبرهای " + CategoryCatalog.localized(item.id, languageTag),
                                style = MaterialTheme.typography.labelSmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                        }
                    }
                }
            }
        }
    }
}

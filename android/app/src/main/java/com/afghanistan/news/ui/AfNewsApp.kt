package com.afghanistan.news.ui

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.unit.LayoutDirection
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import com.afghanistan.news.core.model.Language
import com.afghanistan.news.ui.article.ArticleScreen
import com.afghanistan.news.ui.feed.FeedScreen
import com.afghanistan.news.ui.home.HomeScreen
import com.afghanistan.news.ui.more.MoreScreen
import com.afghanistan.news.ui.navigation.BottomDestination
import com.afghanistan.news.ui.navigation.DeepLink
import com.afghanistan.news.ui.navigation.Routes
import com.afghanistan.news.ui.onboarding.OnboardingScreen
import com.afghanistan.news.ui.saved.SavedScreen
import com.afghanistan.news.ui.search.SearchScreen
import com.afghanistan.news.ui.theme.AfNewsTheme

/**
 * Root composable: theme + bottom-navigation shell + navigation graph.
 * Tabs are top-level; everything else pushes on top and keeps the back stack intact.
 */
@Composable
fun AfNewsApp(
    initialDeepLink: DeepLink?,
    themeMode: String,
    languageTag: String,
    ready: Boolean,
    mainViewModel: MainViewModel = hiltViewModel(),
) {
    AfNewsTheme(themeMode = themeMode, languageTag = languageTag) {
        val navController = rememberNavController()
        val backStackEntry by navController.currentBackStackEntryAsState()
        val currentRoute = backStackEntry?.destination?.route
        val deepLinks by mainViewModel.deepLinks.collectAsStateWithLifecycle(initialValue = null)
        val onboardingRequired by mainViewModel.onboardingRequired.collectAsStateWithLifecycle()

        // Deep links arriving while the process is alive (onNewIntent) are re-routed here.
        LaunchedEffect(deepLinks, ready) {
            val link = deepLinks ?: initialDeepLink ?: return@LaunchedEffect
            if (!ready) return@LaunchedEffect
            when (link) {
                is DeepLink.ArticleLink -> navController.navigate(Routes.article(link.articleId))
                is DeepLink.ProvinceLink -> navController.navigate(Routes.province(link.provinceId))
                is DeepLink.SearchLink -> navController.navigate(Routes.SEARCH)
            }
        }

        // §7.1: the bootstrap routes to onboarding or Home. The app is already usable at this
        // point (the bundled feed pack is unpacked and reference data is local), so onboarding
        // never waits on the network and can always be dismissed.
        if (ready && onboardingRequired) {
            OnboardingScreen(onDone = { mainViewModel.completeOnboarding() })
            return@AfNewsTheme
        }

        Surface(color = MaterialTheme.colorScheme.background, modifier = Modifier.fillMaxSize()) {
            Scaffold(
                bottomBar = {
                    NavigationBar {
                        BottomDestination.entries.forEach { destination ->
                            val selected = currentRoute?.startsWith(destination.route) == true
                            NavigationBarItem(
                                selected = selected,
                                onClick = {
                                    navController.navigate(destination.route) {
                                        popUpTo(navController.graph.findStartDestination().id) { saveState = true }
                                        launchSingleTop = true
                                        restoreState = true
                                    }
                                },
                                icon = { Text(destination.icon) },
                                label = { Text(destination.label, style = MaterialTheme.typography.labelSmall) },
                                alwaysShowLabel = true,
                            )
                        }
                    }
                },
            ) { padding ->
                NavHost(
                    navController = navController,
                    startDestination = Routes.HOME,
                    modifier = Modifier.padding(padding),
                ) {
                    composable(Routes.HOME) {
                        HomeScreen(
                            onOpenArticle = { navController.navigate(Routes.article(it)) },
                            onOpenCategory = { navController.navigate(Routes.category(it)) },
                            onOpenProvince = { navController.navigate(Routes.province(it)) },
                            onOpenNotifications = { navController.navigate(Routes.NOTIFICATIONS) },
                        )
                    }
                    composable(Routes.AFGHANISTAN) {
                        FeedScreen(
                            title = "افغانستان",
                            categoryId = "afghanistan",
                            onOpenArticle = { navController.navigate(Routes.article(it)) },
                        )
                    }
                    composable(Routes.WORLD) {
                        FeedScreen(
                            title = "جهان",
                            categoryId = "world",
                            onOpenArticle = { navController.navigate(Routes.article(it)) },
                        )
                    }
                    composable(Routes.CATEGORY) { entry ->
                        val categoryId = entry.arguments?.getString("categoryId").orEmpty()
                        FeedScreen(
                            title = categoryId,
                            categoryId = categoryId,
                            onOpenArticle = { navController.navigate(Routes.article(it)) },
                        )
                    }
                    composable(Routes.PROVINCE) { entry ->
                        val provinceId = entry.arguments?.getString("provinceId").orEmpty()
                        FeedScreen(
                            title = provinceId,
                            provinceId = provinceId,
                            onOpenArticle = { navController.navigate(Routes.article(it)) },
                        )
                    }
                    composable(Routes.SOURCE) { entry ->
                        val sourceId = entry.arguments?.getString("sourceId").orEmpty()
                        FeedScreen(
                            title = sourceId,
                            sourceId = sourceId,
                            onOpenArticle = { navController.navigate(Routes.article(it)) },
                        )
                    }
                    composable(Routes.SAVED) {
                        SavedScreen(onOpenArticle = { navController.navigate(Routes.article(it)) })
                    }
                    composable(Routes.SEARCH) {
                        SearchScreen(onOpenArticle = { navController.navigate(Routes.article(it)) })
                    }
                    composable(Routes.ARTICLE) { entry ->
                        val id = entry.arguments?.getString("articleId").orEmpty()
                        ArticleScreen(
                            articleId = id,
                            onBack = { navController.popBackStack() },
                            onOpenSource = { navController.navigate(Routes.source(it)) },
                        )
                    }
                    composable(Routes.MORE) {
                        MoreScreen(
                            onOpenProvinces = { navController.navigate(Routes.PROVINCES) },
                            onOpenSources = { navController.navigate(Routes.SOURCES) },
                            onOpenSettings = { navController.navigate(Routes.SETTINGS) },
                            onOpenNotifications = { navController.navigate(Routes.NOTIFICATIONS) },
                            onOpenSearch = { navController.navigate(Routes.SEARCH) },
                            onOpenAbout = { navController.navigate(Routes.ABOUT) },
                            onOpenCategory = { navController.navigate(Routes.category(it)) },
                        )
                    }
                    composable(Routes.PROVINCES) {
                        com.afghanistan.news.ui.more.ProvincesScreen(
                            onOpenProvince = { navController.navigate(Routes.province(it)) },
                        )
                    }
                    composable(Routes.SOURCES) {
                        com.afghanistan.news.ui.more.SourcesScreen(
                            onOpenSource = { navController.navigate(Routes.source(it)) },
                        )
                    }
                    composable(Routes.SETTINGS) {
                        com.afghanistan.news.ui.more.SettingsScreen()
                    }
                    composable(Routes.NOTIFICATIONS) {
                        com.afghanistan.news.ui.more.NotificationsScreen(
                            onOpenArticle = { navController.navigate(Routes.article(it)) },
                        )
                    }
                    composable(Routes.ABOUT) {
                        com.afghanistan.news.ui.more.AboutScreen()
                    }
                }
            }
        }
    }
}

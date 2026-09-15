package com.afghanistan.news.push

import com.afghanistan.news.core.common.AppDispatchers
import com.afghanistan.news.core.database.NewsDatabase
import com.afghanistan.news.core.database.entity.NotificationItemEntity
import com.afghanistan.news.core.datastore.SettingsStore
import com.afghanistan.news.data.sync.RefreshWorker
import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage
import androidx.work.WorkManager
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import java.time.Instant
import javax.inject.Inject
import dagger.hilt.android.AndroidEntryPoint

/**
 * FCM receiver.
 *
 * Contract (§5): notifications are informational, never required — the inbox works offline
 * from Room, so a dropped push is never a lost story. Data-only payloads are preferred so
 * the app controls rendering, translations and the "no account" promise.
 */
@AndroidEntryPoint
class AfNewsMessagingService : FirebaseMessagingService() {

    @Inject lateinit var settings: SettingsStore
    @Inject lateinit var db: NewsDatabase
    @Inject lateinit var dispatchers: AppDispatchers

    override fun onMessageReceived(message: RemoteMessage) {
        val scope = CoroutineScope(dispatchers.io)
        val articleId = message.data["articleId"]
        val type = message.data["type"] ?: "breaking"
        val topic = message.data["topic"]
            ?: message.from?.replace("/topics/", "")?.trim()
            ?: type
        val title = message.data["title"] ?: message.notification?.title ?: "خبر تازه"
        val body = message.data["body"] ?: message.notification?.body
        val id = message.data["id"] ?: message.messageId ?: "$topic-${System.currentTimeMillis()}"

        scope.launch {
            db.notificationDao().upsert(
                listOf(
                    NotificationItemEntity(
                        id = id,
                        articleId = articleId,
                        topic = topic,
                        title = title,
                        body = body,
                        createdAt = Instant.now(),
                        readAt = null,
                    ),
                ),
            )
            // Pull the authoritative article body in the background; the inbox is already
            // readable without it (§5 "no lost story").
            RefreshWorker.refreshNow(WorkManager.getInstance(applicationContext))
        }
    }

    override fun onNewToken(token: String) {
        val scope = CoroutineScope(dispatchers.io)
        scope.launch {
            settings.setPushToken(token)
            // The next PushSyncWorker cycle re-registers the (anonymous) device token.
            com.afghanistan.news.data.sync.PushSyncWorker.schedule(WorkManager.getInstance(applicationContext))
        }
    }
}

// Root build file: declares the plugins once so the app module only applies them.
plugins {
    alias(libs.plugins.android.application) apply false
    alias(libs.plugins.kotlin.android) apply false
    alias(libs.plugins.kotlin.compose) apply false
    alias(libs.plugins.ksp) apply false
    alias(libs.plugins.hilt) apply false
    // google-services is declared but only applied when app/google-services.json exists,
    // so a developer without an FCM project can still build and run the client.
    alias(libs.plugins.google.services) apply false
}

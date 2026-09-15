# Afghanistan News — R8 rules.
# Moshi + Retrofit + Room ship consumer rules; only the model package needs protection
# because it is reflectively decoded and stored.
-keep class com.afghanistan.news.core.network.dto.** { *; }
-keep class com.afghanistan.news.core.model.** { *; }
-keepattributes Signature, InnerClasses, EnclosingMethod, *Annotation*
-keepclassmembers class * extends androidx.room.RoomDatabase { <init>(); }
-dontwarn org.jetbrains.annotations.**

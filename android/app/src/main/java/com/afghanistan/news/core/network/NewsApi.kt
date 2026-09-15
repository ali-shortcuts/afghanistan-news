package com.afghanistan.news.core.network

import com.afghanistan.news.core.network.dto.ArticleDto
import com.afghanistan.news.core.network.dto.ArticlePageDto
import com.afghanistan.news.core.network.dto.CategoryDto
import com.afghanistan.news.core.network.dto.FeedPackVersionDto
import com.afghanistan.news.core.network.dto.HomeDto
import com.afghanistan.news.core.network.dto.NotificationPageDto
import com.afghanistan.news.core.network.dto.ProvinceDto
import com.afghanistan.news.core.network.dto.PushRegisterRequest
import com.afghanistan.news.core.network.dto.PushRegisterResponse
import com.afghanistan.news.core.network.dto.ReferencePageDto
import com.afghanistan.news.core.network.dto.ServerConfigDto
import com.afghanistan.news.core.network.dto.SourceDto
import com.afghanistan.news.core.network.dto.SourcePageDto
import retrofit2.Response
import retrofit2.http.Body
import retrofit2.http.DELETE
import retrofit2.http.GET
import retrofit2.http.POST
import retrofit2.http.Path
import retrofit2.http.Query

/**
 * Public news API. No authentication: v1 deliberately ships without mandatory login (§1, §5).
 * Every list endpoint is cursor-paginated and bounded server-side.
 */
interface NewsApi {

    @GET("v1/home")
    suspend fun home(
        @Query("provinceId") provinceId: String? = null,
        @Query("language") language: String? = null,
    ): Response<HomeDto>

    @GET("v1/articles")
    suspend fun articles(
        @Query("cursor") cursor: String? = null,
        @Query("limit") limit: Int = DEFAULT_PAGE_SIZE,
        @Query("categoryId") categoryId: String? = null,
        @Query("provinceId") provinceId: String? = null,
        @Query("sourceId") sourceId: String? = null,
        @Query("sort") sort: String? = null,
        @Query("language") language: String? = null,
    ): Response<ArticlePageDto>

    @GET("v1/articles/{id}")
    suspend fun article(@Path("id") id: String): Response<ArticleDto>

    @GET("v1/search")
    suspend fun search(
        @Query("q") query: String,
        @Query("cursor") cursor: String? = null,
        @Query("limit") limit: Int = DEFAULT_PAGE_SIZE,
    ): Response<ArticlePageDto>

    @GET("v1/categories")
    suspend fun categories(): Response<ReferencePageDto<CategoryDto>>

    @GET("v1/provinces")
    suspend fun provinces(): Response<ReferencePageDto<ProvinceDto>>

    @GET("v1/provinces/{id}/articles")
    suspend fun provinceArticles(
        @Path("id") provinceId: String,
        @Query("cursor") cursor: String? = null,
        @Query("limit") limit: Int = DEFAULT_PAGE_SIZE,
    ): Response<ArticlePageDto>

    @GET("v1/sources")
    suspend fun sources(
        @Query("cursor") cursor: String? = null,
        @Query("limit") limit: Int = 50,
    ): Response<SourcePageDto>

    @GET("v1/sources/{id}")
    suspend fun source(@Path("id") id: String): Response<SourceDto>

    @GET("v1/clusters/{id}")
    suspend fun cluster(@Path("id") id: String): Response<ArticlePageDto>

    @GET("v1/notifications")
    suspend fun notifications(@Query("limit") limit: Int = 50): Response<NotificationPageDto>

    @GET("v1/feed-pack/version")
    suspend fun feedPackVersion(): Response<FeedPackVersionDto>

    @GET("v1/config")
    suspend fun config(): Response<ServerConfigDto>

    @POST("v1/push/register")
    suspend fun registerPush(@Body body: PushRegisterRequest): Response<PushRegisterResponse>

    @DELETE("v1/push/registrations/{id}")
    suspend fun unregisterPush(@Path("id") registrationId: String): Response<Unit>

    companion object {
        const val DEFAULT_PAGE_SIZE = 30
    }
}

package com.groovesync.common.syncd

import com.groovesync.common.sync.ResumableDownloader
import kotlinx.serialization.json.Json
import java.io.File
import java.io.IOException
import java.net.URLEncoder
import java.security.MessageDigest

class ApiException(val status: Int, message: String) : IOException(message)

class SyncdApi(
    private val serverUrls: List<String>,
    private var token: String = "",
    private val deviceId: String = "",
) {
    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true; coerceInputValues = true }
    var lastBaseUrl: String = serverUrls.firstOrNull().orEmpty()
        private set

    /** The base a request actually succeeded on, so a stream skips dead URLs. */
    private var reachableBaseUrl: String? = null

    fun withToken(value: String): SyncdApi = SyncdApi(serverUrls, value, deviceId)

    private fun headers(extra: Map<String, String> = emptyMap()): Map<String, String> {
        val out = linkedMapOf("Accept" to "application/json", "Accept-Encoding" to "gzip")
        if (token.isNotEmpty()) out["Authorization"] = "Bearer $token"
        if (deviceId.isNotEmpty()) out["X-Device-Id"] = deviceId
        out.putAll(extra)
        return out
    }

    private fun call(
        method: String,
        path: String,
        extraHeaders: Map<String, String> = emptyMap(),
        body: ByteArray? = null,
    ): HttpResponse {
        var last: Exception? = null
        for ((index, base) in serverUrls.withIndex()) {
            try {
                val response = Http.request(method, base.trimEnd('/') + path, headers(extraHeaders), body)
                if (response.status >= 500 && index < serverUrls.lastIndex) {
                    last = IOException("${base} returned ${response.status}")
                    continue
                }
                lastBaseUrl = base
                reachableBaseUrl = base
                return response
            } catch (error: Exception) {
                last = error
            }
        }
        throw last ?: IOException("no server urls configured")
    }

    private fun jsonBody(body: String): ByteArray = body.toByteArray(Charsets.UTF_8)

    private fun encode(value: String): String =
        URLEncoder.encode(value, "UTF-8").replace("+", "%20")

    private fun requireOk(response: HttpResponse, what: String): HttpResponse {
        if (response.status !in 200..299) {
            throw ApiException(response.status, "$what failed: ${response.status} ${response.text().take(300)}")
        }
        return response
    }

    fun register(
        adminToken: String,
        id: String,
        name: String,
        platform: String,
        syncMode: String,
        policy: PolicyDto,
        urls: List<String>,
    ): RegisterResponse {
        val request = RegisterRequest(id, name, platform, 1, syncMode, policy, urls)
        val response = SyncdApi(urls, adminToken).call(
            "POST",
            "/sync/v1/devices",
            mapOf("Content-Type" to "application/json"),
            jsonBody(json.encodeToString(RegisterRequest.serializer(), request)),
        )
        requireOk(response, "register")
        return json.decodeFromString(RegisterResponse.serializer(), response.text())
    }

    fun meta(): String = requireOk(call("GET", "/sync/v1/meta"), "meta").text()

    fun metaInfo(): MetaDto {
        val response = requireOk(call("GET", "/sync/v1/meta"), "meta")
        return json.decodeFromString(MetaDto.serializer(), response.text())
    }

    fun library(limit: Int = 500, offset: Int = 0): LibraryResponse {
        val response = requireOk(call("GET", "/sync/v1/library?limit=$limit&offset=$offset"), "library")
        return json.decodeFromString(LibraryResponse.serializer(), response.text())
    }

    fun libraryRevision(): String? {
        val response = call("GET", "/sync/v1/library/revision")
        if (response.status == 404 || response.status == 501) return null
        requireOk(response, "library revision")
        return json.decodeFromString(LibraryRevisionResponse.serializer(), response.text()).revision
    }

    fun plan(deviceId: String, onWiFi: Boolean = false): PlanResponse {
        val extra = if (onWiFi) mapOf("X-Network" to "wifi") else emptyMap()
        val response = requireOk(call("GET", "/sync/v1/devices/$deviceId/plan", extra), "plan")
        return json.decodeFromString(PlanResponse.serializer(), response.text())
    }

    fun setPolicy(deviceId: String, policy: PolicyDto) {
        val response = call(
            "PUT",
            "/sync/v1/devices/$deviceId/policy",
            mapOf("Content-Type" to "application/json"),
            jsonBody(json.encodeToString(PolicyDto.serializer(), policy)),
        )
        requireOk(response, "policy")
    }

    fun setSyncMode(deviceId: String, mode: String) {
        val response = call(
            "PUT",
            "/sync/v1/devices/$deviceId/sync-mode",
            mapOf("Content-Type" to "application/json"),
            jsonBody(json.encodeToString(SyncModeRequest.serializer(), SyncModeRequest(mode))),
        )
        requireOk(response, "sync mode")
    }

    fun reportHoldings(deviceId: String, holdings: List<HoldingDto>) {
        val response = call(
            "PUT",
            "/sync/v1/devices/$deviceId/holdings",
            mapOf("Content-Type" to "application/json"),
            jsonBody(json.encodeToString(HoldingsRequest.serializer(), HoldingsRequest(holdings))),
        )
        if (response.status != 204 && response.status !in 200..299) {
            throw ApiException(response.status, "holdings failed: ${response.status} ${response.text().take(300)}")
        }
    }

    fun events(since: String, limit: Int = 200): EventsResponse {
        val suffix = if (since.isBlank()) "" else "&since=${URLEncoder.encode(since, "UTF-8")}"
        val response = requireOk(call("GET", "/sync/v1/events?limit=$limit$suffix"), "events")
        return json.decodeFromString(EventsResponse.serializer(), response.text())
    }

    /**
     * Absolute URL that streams the best rendition of a canonical track. Uses
     * the server that answered last, so a mesh failover is already reflected.
     * Returns null when no server is configured.
     */
    fun streamUrl(canonicalId: String): String? {
        if (canonicalId.isBlank()) return null
        val base = reachableBaseUrl ?: lastBaseUrl.ifBlank { serverUrls.firstOrNull().orEmpty() }
        if (base.isBlank()) return null
        return base.trimEnd('/') + "/sync/v1/library/tracks/" + encode(canonicalId) + "/audio"
    }

    /** Headers for a media request: the JSON calls' auth, without gzip. */
    fun streamHeaders(): Map<String, String> {
        val out = linkedMapOf<String, String>()
        if (token.isNotEmpty()) out["Authorization"] = "Bearer $token"
        if (deviceId.isNotEmpty()) out["X-Device-Id"] = deviceId
        return out
    }

    fun eventsStream(): SseStream {
        val base = lastBaseUrl.ifBlank { serverUrls.firstOrNull().orEmpty() }
        if (base.isBlank()) throw IOException("no server urls configured")
        return SseStream(
            url = base.trimEnd('/') + "/sync/v1/events/stream",
            headers = headers() - "Accept",
        )
    }

    fun appendEvents(events: List<EventDto>) {
        val response = call(
            "POST",
            "/sync/v1/events",
            mapOf("Content-Type" to "application/json"),
            jsonBody(json.encodeToString(EventsRequest.serializer(), EventsRequest(events))),
        )
        requireOk(response, "append events")
    }

    fun uploadStatus(hash: String): UploadStatus {
        val response = requireOk(call("GET", "/sync/v1/library/uploads/$hash"), "upload status")
        return json.decodeFromString(UploadStatus.serializer(), response.text())
    }

    fun upload(hash: String, name: String, track: String?, file: File, chunkSize: Int = 4 * 1024 * 1024): UploadStatus {
        val bytes = file.readBytes()
        var start = 0
        while (start < bytes.size) {
            val end = minOf(start + chunkSize - 1, bytes.lastIndex)
            val chunk = bytes.copyOfRange(start, end + 1)
            val params = buildString {
                append("?name=").append(URLEncoder.encode(name, "UTF-8"))
                if (!track.isNullOrBlank()) append("&track=").append(URLEncoder.encode(track, "UTF-8"))
            }
            val response = call(
                "PUT",
                "/sync/v1/library/uploads/$hash$params",
                mapOf(
                    "Content-Type" to "application/octet-stream",
                    "Content-Range" to "bytes $start-$end/${bytes.size}",
                ),
                chunk,
            )
            if (response.status != 201 && response.status != 202) {
                throw ApiException(response.status, "upload failed: ${response.status} ${response.text().take(300)}")
            }
            start = end + 1
        }
        repeat(120) {
            val status = uploadStatus(hash)
            if (status.complete) return status
            Thread.sleep(250)
        }
        throw IOException("upload did not complete: $hash")
    }

    fun download(hash: String, destination: File, expectedSize: Long? = null): File {
        val base = lastBaseUrl.ifBlank { serverUrls.firstOrNull().orEmpty() }
        if (base.isBlank()) throw IOException("no server urls configured")
        ResumableDownloader().download(
            url = base.trimEnd('/') + "/sync/v1/library/files/$hash",
            destination = destination,
            headers = headers() - "Accept",
            expectedSize = expectedSize,
            expectedSha256 = hash,
        )
        lastBaseUrl = base
        return destination
    }

    fun review(limit: Int = 100, offset: Int = 0): String =
        requireOk(call("GET", "/sync/v1/review?limit=$limit&offset=$offset"), "review").text()

    fun reviewQueue(limit: Int = 100, offset: Int = 0): List<ReviewItemDto> {
        val response = requireOk(call("GET", "/sync/v1/review?limit=$limit&offset=$offset"), "review")
        return json.decodeFromString(ReviewResponse.serializer(), response.text()).items
    }

    fun resolveReview(id: Long) {
        requireOk(call("POST", "/sync/v1/review/$id/resolve"), "review resolve")
    }

    fun deleteReview(id: Long) {
        requireOk(call("DELETE", "/sync/v1/review/$id"), "review delete")
    }

    fun why(canonicalId: String): WhyDto {
        val response = requireOk(call("GET", "/sync/v1/why/${encode(canonicalId)}"), "why")
        return json.decodeFromString(WhyDto.serializer(), response.text())
    }

    fun setOverride(canonicalId: String, level: String) {
        val response = call(
            "PUT",
            "/sync/v1/why/${encode(canonicalId)}/override",
            mapOf("Content-Type" to "application/json"),
            jsonBody("""{"value":"$level"}"""),
        )
        requireOk(response, "override")
    }

    fun deleteOverride(canonicalId: String) {
        requireOk(call("DELETE", "/sync/v1/why/${encode(canonicalId)}/override"), "override")
    }

    fun health(): String = requireOk(call("GET", "/sync/v1/health"), "health").text()

    fun settings(): String = requireOk(call("GET", "/sync/v1/settings"), "settings").text()

    companion object {
        fun sha256(file: File): String {
            val digest = MessageDigest.getInstance("SHA-256")
            file.inputStream().use { input ->
                val buffer = ByteArray(64 * 1024)
                while (true) {
                    val read = input.read(buffer)
                    if (read < 0) break
                    digest.update(buffer, 0, read)
                }
            }
            return digest.digest().joinToString("") { "%02x".format(it) }
        }
    }
}

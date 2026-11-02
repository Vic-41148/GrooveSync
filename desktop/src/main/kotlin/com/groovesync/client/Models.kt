package com.groovesync.client

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class PolicyDto(
    val scope: String = "everything",
    @SerialName("affinity_threshold") val affinityThreshold: Double = 0.0,
    @SerialName("storage_cap_bytes") val storageCapBytes: Long = 0,
    @SerialName("network_rule") val networkRule: String = "any",
    @SerialName("mirror_deletions") val mirrorDeletions: Boolean = false,
    val quality: String = "original",
)

@Serializable
data class RegisterRequest(
    val id: String,
    val name: String,
    val platform: String,
    val build: Int = 1,
    @SerialName("sync_mode") val syncMode: String,
    val policy: PolicyDto,
    val urls: List<String>,
)

@Serializable
data class DeviceDto(
    val id: String = "",
    val name: String = "",
    val platform: String = "",
    val build: Int = 0,
    @SerialName("sync_mode") val syncMode: String = "",
    val policy: PolicyDto = PolicyDto(),
    val urls: List<String> = emptyList(),
)

@Serializable
data class RegisterResponse(val device: DeviceDto, val token: String)

@Serializable
data class ContributionDto(val path: String = "", val source: String = "")

@Serializable
data class RenditionDto(
    val kind: String = "",
    val path: String = "",
    val codec: String = "",
    val health: String = "",
    val contributions: List<ContributionDto> = emptyList(),
)

@Serializable
data class TrackDto(
    @SerialName("canonical_id") val canonicalId: String,
    val title: String = "",
    val artist: String = "",
    val album: String = "",
    @SerialName("duration_ms") val durationMs: Int = 0,
    val renditions: List<RenditionDto> = emptyList(),
)

@Serializable
data class LibraryResponse(
    val limit: Int = 0,
    val offset: Int = 0,
    val total: Int = 0,
    val tracks: List<TrackDto> = emptyList(),
)

@Serializable
data class FileDto(
    @SerialName("canonical_id") val canonicalId: String,
    val hash: String,
    val path: String = "",
    @SerialName("size_bytes") val sizeBytes: Long = 0,
)

@Serializable
data class PlanResponse(
    @SerialName("device_id") val deviceId: String = "",
    val downloads: List<FileDto> = emptyList(),
    val deletes: List<FileDto> = emptyList(),
    val evictions: List<FileDto> = emptyList(),
    val unchanged: Int = 0,
    @SerialName("download_bytes") val downloadBytes: Long = 0,
    val warnings: List<String> = emptyList(),
)

@Serializable
data class HoldingDto(
    @SerialName("canonical_id") val canonicalId: String,
    val hash: String,
)

@Serializable
data class HoldingsRequest(val holdings: List<HoldingDto>)

@Serializable
data class HlcDto(val wall: String, val counter: Long = 0, val node: String)

@Serializable
data class EventDto(
    val id: String,
    @SerialName("device_id") val deviceId: String,
    @SerialName("canonical_id") val canonicalId: String,
    val source: String = "local",
    val type: String,
    val value: Double = 0.0,
    @SerialName("percent_played") val percentPlayed: Double = 0.0,
    @SerialName("position_ms") val positionMs: Long = 0,
    val hlc: HlcDto,
)

@Serializable
data class EventsRequest(val events: List<EventDto>)

@Serializable
data class EventsResponse(
    val events: List<EventDto> = emptyList(),
    val next: String = "",
    @SerialName("has_more") val hasMore: Boolean = false,
)

@Serializable
data class UploadStatus(
    val hash: String = "",
    val received: Long = 0,
    val total: Long = 0,
    val complete: Boolean = false,
    val path: String = "",
)

@Serializable
data class ClientState(
    val deviceId: String = "",
    val token: String = "",
    val serverUrls: List<String> = emptyList(),
    val syncMode: String = "app",
    val policy: PolicyDto = PolicyDto(),
    val cursor: String = "",
    val outbox: List<EventDto> = emptyList(),
    val holdings: List<HoldingDto> = emptyList(),
    val liked: Set<String> = emptySet(),
    val recentlyPlayed: Map<String, String> = emptyMap(),
    val canonicalByHash: Map<String, String> = emptyMap(),
    val lastSync: String = "",
)

data class SyncReport(
    val baseUrl: String = "",
    val pulled: Int = 0,
    val pushed: Int = 0,
    val downloaded: Int = 0,
    val uploaded: Int = 0,
    val deleted: Int = 0,
    val evicted: Int = 0,
    val holdings: Int = 0,
    val warnings: List<String> = emptyList(),
)

package com.groovesync.common.net

data class ServerUrls(val lan: String?, val mesh: String?, val public: String?) {
    fun ordered(): List<String> = listOfNotNull(lan, mesh, public)
}

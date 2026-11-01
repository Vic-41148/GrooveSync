package com.groovesync.common.upload

data class UploadContribution(val canonicalId: String, val hash: String, val path: String, val parts: Int)

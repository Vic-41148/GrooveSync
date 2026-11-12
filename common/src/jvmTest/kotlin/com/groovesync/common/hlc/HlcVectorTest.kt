package com.groovesync.common.hlc

import java.io.File
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class HlcVectorTest {
    @Test
    fun vectors() {
        val path = findVectors()
        val pattern = Regex(
            """\{\s*"a"\s*:\s*"([^"]+)"\s*,\s*"b"\s*:\s*"([^"]+)"\s*,\s*"max"\s*:\s*"([^"]+)"\s*\}""",
        )
        val cases = pattern.findAll(path.readText())
            .map { it.groupValues[1] to it.groupValues[2] to it.groupValues[3] }
            .toList()
        assertTrue(cases.isNotEmpty(), "no vectors in $path")
        for ((pair, max) in cases) {
            val (a, b) = pair
            val actual = if (Hlc.decode(a) >= Hlc.decode(b)) a else b
            assertEquals(max, actual, "Max($a,$b)")
        }
    }

    private fun findVectors(): File {
        var dir: File? = File(System.getProperty("user.dir")).absoluteFile
        while (dir != null) {
            val candidate = File(dir, "spec/hlc_vectors.json")
            if (candidate.isFile) return candidate
            dir = dir.parentFile
        }
        error("spec/hlc_vectors.json not found above ${System.getProperty("user.dir")}")
    }
}

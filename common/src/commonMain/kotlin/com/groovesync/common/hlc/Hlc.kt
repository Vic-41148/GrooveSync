package com.groovesync.common.hlc

data class Hlc(val wall: Long, val counter: Int, val node: String) : Comparable<Hlc> {
    override fun compareTo(other: Hlc): Int {
        if (wall != other.wall) return wall.compareTo(other.wall)
        if (counter != other.counter) return counter.compareTo(other.counter)
        return node.compareTo(other.node)
    }

    fun encode(): String = "$wall:$counter:$node"

    companion object {
        fun decode(s: String): Hlc {
            val parts = s.split(":")
            require(parts.size == 3)
            return Hlc(parts[0].toLong(), parts[1].toInt(), parts[2])
        }
    }
}

class HlcClock(private val nodeId: String, private val now: () -> Long) {
    private var last: Hlc = Hlc(0, 0, nodeId)

    @Synchronized
    fun tick(): Hlc {
        val t = now()
        last = if (t > last.wall) Hlc(t, 0, nodeId) else Hlc(last.wall, last.counter + 1, nodeId)
        return last
    }

    @Synchronized
    fun receive(remote: Hlc): Hlc {
        val t = now()
        val wall = maxOf(t, last.wall, remote.wall)
        val counter = when {
            wall == last.wall && wall == remote.wall -> maxOf(last.counter, remote.counter) + 1
            wall == last.wall -> last.counter + 1
            wall == remote.wall -> remote.counter + 1
            else -> 0
        }
        last = Hlc(wall, counter, nodeId)
        return last
    }
}

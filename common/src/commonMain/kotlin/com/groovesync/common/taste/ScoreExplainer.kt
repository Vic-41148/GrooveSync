package com.groovesync.common.taste

data class Explanation(val name: String, val value: Double)

object ScoreExplainer {
    fun explain(contributions: List<Pair<String, Double>>): List<Explanation> =
        contributions.map { Explanation(it.first, it.second) }
}

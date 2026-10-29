package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// metaResponse is the handshake a client makes before it trusts anything else.
// Field names are part of the wire contract, not free to rename.
type metaResponse struct {
	ServerVersion    string         `json:"server_version"`
	Proto            int            `json:"proto"`
	ProtoMin         int            `json:"proto_min"`
	Schema           int64          `json:"schema"`
	MinBuild         map[string]int `json:"min_build"`
	RecommendedBuild map[string]int `json:"recommended_build"`
	Features         []string       `json:"features"`
	ReleasesURL      string         `json:"releases_url"`
}

func (d Deps) handleMeta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, metaResponse{
		ServerVersion:    d.Version,
		Proto:            d.Proto,
		ProtoMin:         protoMin(d.Proto),
		Schema:           d.Schema,
		MinBuild:         mapOrEmpty(d.MinBuild),
		RecommendedBuild: mapOrEmpty(d.RecommendedBuild),
		Features:         sliceOrEmpty(d.Features),
		ReleasesURL:      d.ReleasesURL,
	}, time.Minute)
}

// protoMin keeps proto-1 readable for at least 90 days after a bump, so a
// client that missed an update still gets a version it can parse.
func protoMin(proto int) int {
	if proto < 2 {
		return 1
	}
	return proto - 1
}

func mapOrEmpty(m map[string]int) map[string]int {
	if m == nil {
		return map[string]int{}
	}
	return m
}

// A nil slice marshals as null, which breaks clients that iterate features.
func sliceOrEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// writeJSON marshals before writing so a bad payload cannot leave a 200 with a
// half-written body. maxAge of zero means the response must not be cached.
func writeJSON(w http.ResponseWriter, status int, v any, maxAge time.Duration) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, `{"error":"encode"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if maxAge > 0 {
		w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(int(maxAge.Seconds())))
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.WriteHeader(status)
	w.Write(append(body, '\n'))
}

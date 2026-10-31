// Package transfer plans the files one device should receive or drop.
package transfer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Vic-41148/groovesync/internal/devices"
	"github.com/Vic-41148/groovesync/internal/taste"
)

// File is one content-addressed file in a transfer plan.
type File struct {
	CanonicalID string `json:"canonical_id"`
	Hash        string `json:"hash"`
	Path        string `json:"path"`
	SizeBytes   int64  `json:"size_bytes"`
}

// Plan is what one device should download and drop.
type Plan struct {
	DeviceID      string   `json:"device_id"`
	Downloads     []File   `json:"downloads"`
	Deletes       []File   `json:"deletes"`
	Unchanged     int      `json:"unchanged"`
	DownloadBytes int64    `json:"download_bytes"`
	Warnings      []string `json:"warnings"`
}

// DeviceSource reads the device and the files it already holds.
type DeviceSource interface {
	Get(context.Context, string) (devices.Device, error)
	ListHoldings(context.Context, string) ([]devices.Holding, error)
}

// Planner builds a plan for one device from its policy and the mirrored
// library.
type Planner struct {
	DB      *sql.DB
	Devices DeviceSource
	Now     func() time.Time
}

type target struct {
	CanonicalID string
	Score       float64
}

type rendition struct {
	CanonicalID string
	Hash        string
	Path        string
	SizeBytes   int64
	Codec       string
}

// Plan compares the device policy and holdings with the library and returns the
// transfers that bring the device in line. It never deletes unless the policy
// allows mirrored deletions.
func (p Planner) Plan(ctx context.Context, deviceID string) (Plan, error) {
	if p.DB == nil {
		return Plan{}, errors.New("transfer: database is required")
	}
	if p.Devices == nil {
		return Plan{}, errors.New("transfer: device source is required")
	}
	device, err := p.Devices.Get(ctx, deviceID)
	if err != nil {
		return Plan{}, err
	}

	plan := Plan{DeviceID: deviceID, Downloads: []File{}, Deletes: []File{}, Warnings: []string{}}
	if device.SyncMode == devices.ModeOff || device.Policy.Scope == devices.ScopeOff {
		return plan, nil
	}
	if device.Policy.Scope == devices.ScopePlaylists {
		plan.Warnings = append(plan.Warnings, "playlist scope has no playlist source yet")
		return plan, nil
	}

	holdings, err := p.Devices.ListHoldings(ctx, deviceID)
	if err != nil {
		return Plan{}, err
	}
	renditions, err := p.renditions(ctx)
	if err != nil {
		return Plan{}, err
	}
	targets, err := p.targets(ctx, device.Policy)
	if err != nil {
		return Plan{}, err
	}

	held := make(map[string]devices.Holding, len(holdings))
	heldTracks := make(map[string]bool, len(holdings))
	var used int64
	for _, holding := range holdings {
		held[holding.Hash] = holding
		heldTracks[holding.CanonicalID] = true
		used += sizeOf(renditions[holding.CanonicalID], holding.Hash)
	}

	inScope := make(map[string]bool, len(targets))
	for _, target := range targets {
		inScope[target.CanonicalID] = true
	}

	var downloadBytes int64
	capped := false
	for _, target := range targets {
		choice, ok := choose(renditions[target.CanonicalID], device.Policy.Quality)
		if !ok {
			continue
		}
		if _, present := held[choice.Hash]; present {
			plan.Unchanged++
			continue
		}
		if heldTracks[target.CanonicalID] {
			plan.Unchanged++
			continue
		}
		if device.Policy.StorageCapBytes > 0 && used+choice.SizeBytes > device.Policy.StorageCapBytes {
			capped = true
			continue
		}
		used += choice.SizeBytes
		downloadBytes += choice.SizeBytes
		plan.Downloads = append(plan.Downloads, File{
			CanonicalID: target.CanonicalID,
			Hash:        choice.Hash,
			Path:        choice.Path,
			SizeBytes:   choice.SizeBytes,
		})
	}
	if capped {
		plan.Warnings = append(plan.Warnings, "storage cap leaves some tracks unplanned")
	}
	plan.DownloadBytes = downloadBytes

	if device.Policy.MirrorDeletions {
		for _, holding := range holdings {
			if inScope[holding.CanonicalID] {
				continue
			}
			plan.Deletes = append(plan.Deletes, File{
				CanonicalID: holding.CanonicalID,
				Hash:        holding.Hash,
				Path:        pathOf(renditions[holding.CanonicalID], holding.Hash),
				SizeBytes:   sizeOf(renditions[holding.CanonicalID], holding.Hash),
			})
		}
		sort.SliceStable(plan.Deletes, func(i, j int) bool {
			if plan.Deletes[i].CanonicalID == plan.Deletes[j].CanonicalID {
				return plan.Deletes[i].Hash < plan.Deletes[j].Hash
			}
			return plan.Deletes[i].CanonicalID < plan.Deletes[j].CanonicalID
		})
	}
	return plan, nil
}

func (p Planner) targets(ctx context.Context, policy devices.Policy) ([]target, error) {
	switch policy.Scope {
	case devices.ScopeEverything:
		return p.allTracks(ctx)
	case devices.ScopeLiked:
		return p.likedTracks(ctx)
	case devices.ScopeAffinity:
		return p.affinityTracks(ctx, policy.AffinityThreshold)
	default:
		return nil, fmt.Errorf("transfer: unsupported scope %q", policy.Scope)
	}
}

func (p Planner) allTracks(ctx context.Context) ([]target, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT canonical_id FROM track ORDER BY canonical_id ASC`)
	if err != nil {
		return nil, fmt.Errorf("transfer: list tracks: %w", err)
	}
	defer rows.Close()

	var out []target
	for rows.Next() {
		var target target
		if err := rows.Scan(&target.CanonicalID); err != nil {
			return nil, fmt.Errorf("transfer: scan track: %w", err)
		}
		out = append(out, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("transfer: iterate tracks: %w", err)
	}
	return out, nil
}

func (p Planner) likedTracks(ctx context.Context) ([]target, error) {
	rows, err := p.DB.QueryContext(ctx, `
SELECT DISTINCT canonical_id
FROM service_state
WHERE base_state = 'liked'
ORDER BY canonical_id ASC
`)
	if err != nil {
		return nil, fmt.Errorf("transfer: list liked tracks: %w", err)
	}
	defer rows.Close()

	var out []target
	for rows.Next() {
		var target target
		if err := rows.Scan(&target.CanonicalID); err != nil {
			return nil, fmt.Errorf("transfer: scan liked track: %w", err)
		}
		out = append(out, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("transfer: iterate liked tracks: %w", err)
	}
	return out, nil
}

func (p Planner) affinityTracks(ctx context.Context, threshold float64) ([]target, error) {
	events, err := taste.NewStore(p.DB).List(ctx)
	if err != nil {
		return nil, err
	}
	asOf := time.Now().UTC()
	if p.Now != nil {
		asOf = p.Now().UTC()
	}
	var out []target
	for _, scored := range taste.NewLog(events...).ScoreTracks(taste.DefaultConfig(), asOf) {
		if scored.Score < threshold {
			continue
		}
		out = append(out, target{CanonicalID: scored.CanonicalID, Score: scored.Score})
	}
	return out, nil
}

func (p Planner) renditions(ctx context.Context) (map[string][]rendition, error) {
	rows, err := p.DB.QueryContext(ctx, `
SELECT canonical_id, lower(sha256), path_or_uri, size_bytes, COALESCE(codec, '')
FROM rendition
WHERE kind = 'file' AND sha256 IS NOT NULL AND sha256 <> ''
ORDER BY id ASC
`)
	if err != nil {
		return nil, fmt.Errorf("transfer: list renditions: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]rendition)
	for rows.Next() {
		var item rendition
		if err := rows.Scan(&item.CanonicalID, &item.Hash, &item.Path, &item.SizeBytes, &item.Codec); err != nil {
			return nil, fmt.Errorf("transfer: scan rendition: %w", err)
		}
		out[item.CanonicalID] = append(out[item.CanonicalID], item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("transfer: iterate renditions: %w", err)
	}
	return out, nil
}

func choose(items []rendition, quality devices.Quality) (rendition, bool) {
	if len(items) == 0 {
		return rendition{}, false
	}
	best := items[0]
	for _, item := range items[1:] {
		if preferred(item, best, quality) {
			best = item
		}
	}
	return best, true
}

func preferred(candidate, current rendition, quality devices.Quality) bool {
	switch quality {
	case devices.QualityDataSaver:
		if candidate.SizeBytes == current.SizeBytes {
			return candidate.Codec > current.Codec
		}
		if candidate.SizeBytes <= 0 {
			return false
		}
		if current.SizeBytes <= 0 {
			return true
		}
		return candidate.SizeBytes < current.SizeBytes
	case devices.QualityHigh:
		if lossless(candidate.Codec) != lossless(current.Codec) {
			return lossless(candidate.Codec)
		}
		return candidate.SizeBytes > current.SizeBytes
	default:
		return candidate.SizeBytes > current.SizeBytes
	}
}

func lossless(codec string) bool {
	switch strings.ToLower(codec) {
	case "flac", "alac", "wav", "aiff", "ape", "wv", "tak", "dsf", "dff":
		return true
	default:
		return false
	}
}

func sizeOf(items []rendition, hash string) int64 {
	for _, item := range items {
		if item.Hash == hash {
			return item.SizeBytes
		}
	}
	return 0
}

func pathOf(items []rendition, hash string) string {
	for _, item := range items {
		if item.Hash == hash {
			return item.Path
		}
	}
	return ""
}

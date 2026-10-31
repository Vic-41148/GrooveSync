// Package devices stores the device registry and issues per-device tokens.
package devices

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// SyncMode is how files reach one device.
type SyncMode string

const (
	ModeApp      SyncMode = "app"
	ModeExternal SyncMode = "external"
	ModeOff      SyncMode = "off"
)

// Scope is what a device receives.
type Scope string

const (
	ScopeEverything Scope = "everything"
	ScopeLiked      Scope = "liked"
	ScopeAffinity   Scope = "affinity"
	ScopePlaylists  Scope = "playlists"
	ScopeOff        Scope = "off"
)

// NetworkRule limits transfers to a kind of connection.
type NetworkRule string

const (
	NetworkAny  NetworkRule = "any"
	NetworkWiFi NetworkRule = "wifi"
)

// Quality is the preferred encoding for transfers.
type Quality string

const (
	QualityOriginal  Quality = "original"
	QualityHigh      Quality = "high"
	QualityDataSaver Quality = "data_saver"
)

// Policy is how one device receives files. A zero value means the defaults:
// everything, any network, original quality, no cap and no deletions.
type Policy struct {
	Scope             Scope
	AffinityThreshold float64
	StorageCapBytes   int64
	NetworkRule       NetworkRule
	MirrorDeletions   bool
	Quality           Quality
}

// Device is one registered client.
type Device struct {
	ID       string
	Name     string
	Platform string
	Build    int
	SyncMode SyncMode
	Policy   Policy
	// URLs is the ordered server list the device tries, LAN first.
	URLs      []string
	CreatedAt time.Time
	LastSeen  time.Time
}

// Registration is a stored device together with its freshly issued token.
type Registration struct {
	Device Device
	Token  string
}

// ErrDeviceNotFound reports an unknown device id.
var ErrDeviceNotFound = errors.New("devices: device not found")

// ErrInvalidDevice reports caller-supplied device fields that cannot be stored.
var ErrInvalidDevice = errors.New("devices: invalid device")

// ErrInvalidHolding reports a holding entry that cannot be stored.
var ErrInvalidHolding = errors.New("devices: invalid holding")

// Holding is one file a device reported, keyed by its content hash.
type Holding struct {
	CanonicalID string
	Hash        string
	LastSeen    time.Time
}

// Repo persists the device registry.
type Repo struct {
	DB    *sql.DB
	Now   func() time.Time
	Token func() (string, error)
}

// Register stores a device and rotates its token. The plaintext token is
// returned once; only its hash is persisted.
func (r Repo) Register(ctx context.Context, device Device) (Registration, error) {
	if r.DB == nil {
		return Registration{}, errors.New("devices: database is required")
	}
	device.ID = strings.TrimSpace(device.ID)
	if device.ID == "" {
		return Registration{}, fmt.Errorf("%w: id is required", ErrInvalidDevice)
	}
	device.Name = strings.TrimSpace(device.Name)
	if device.Name == "" {
		return Registration{}, fmt.Errorf("%w: name is required", ErrInvalidDevice)
	}
	if device.Platform == "" {
		device.Platform = "android"
	}
	if device.Build < 0 {
		return Registration{}, fmt.Errorf("%w: build must not be negative", ErrInvalidDevice)
	}
	if device.SyncMode == "" {
		device.SyncMode = ModeApp
	}
	if !validMode(device.SyncMode) {
		return Registration{}, fmt.Errorf("%w: unsupported sync mode %q", ErrInvalidDevice, device.SyncMode)
	}
	policy, err := normalizePolicy(device.Policy, device.SyncMode)
	if err != nil {
		return Registration{}, err
	}
	device.Policy = policy
	device.URLs, err = normalizeURLs(device.URLs)
	if err != nil {
		return Registration{}, err
	}
	urls, err := json.Marshal(device.URLs)
	if err != nil {
		return Registration{}, fmt.Errorf("devices: encode urls: %w", err)
	}

	token, err := r.token()
	if err != nil {
		return Registration{}, err
	}
	now := r.now()
	if _, err := r.DB.ExecContext(ctx, `
INSERT INTO device (
    id, name, platform, build, last_seen, token_hash, sync_mode,
    scope, affinity_threshold, storage_cap_bytes, network_rule, mirror_deletions, quality, urls
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    platform = excluded.platform,
    build = excluded.build,
    last_seen = excluded.last_seen,
    token_hash = excluded.token_hash,
    sync_mode = excluded.sync_mode,
    scope = excluded.scope,
    affinity_threshold = excluded.affinity_threshold,
    storage_cap_bytes = excluded.storage_cap_bytes,
    network_rule = excluded.network_rule,
    mirror_deletions = excluded.mirror_deletions,
    quality = excluded.quality,
    urls = excluded.urls
`, device.ID, device.Name, device.Platform, device.Build, now.Format(time.RFC3339Nano), hashToken(token), string(device.SyncMode),
		string(policy.Scope), policy.AffinityThreshold, policy.StorageCapBytes, string(policy.NetworkRule), boolInt(policy.MirrorDeletions), string(policy.Quality), string(urls)); err != nil {
		return Registration{}, fmt.Errorf("devices: register %s: %w", device.ID, err)
	}

	device.LastSeen = now
	if device.CreatedAt.IsZero() {
		device.CreatedAt = now
	}
	return Registration{Device: device, Token: token}, nil
}

// List returns every registered device without token material.
func (r Repo) List(ctx context.Context) ([]Device, error) {
	if r.DB == nil {
		return nil, errors.New("devices: database is required")
	}
	rows, err := r.DB.QueryContext(ctx, `
SELECT id, name, platform, build, sync_mode, scope, affinity_threshold, storage_cap_bytes, network_rule, mirror_deletions, quality, urls, created_at, last_seen
FROM device
ORDER BY id ASC
`)
	if err != nil {
		return nil, fmt.Errorf("devices: list: %w", err)
	}
	defer rows.Close()

	var out []Device
	for rows.Next() {
		device, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, device)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("devices: iterate: %w", err)
	}
	return out, nil
}

// Get returns one device without token material.
func (r Repo) Get(ctx context.Context, id string) (Device, error) {
	if r.DB == nil {
		return Device{}, errors.New("devices: database is required")
	}
	device, err := scanDevice(r.DB.QueryRowContext(ctx, `
SELECT id, name, platform, build, sync_mode, scope, affinity_threshold, storage_cap_bytes, network_rule, mirror_deletions, quality, urls, created_at, last_seen
FROM device
WHERE id = ?
`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrDeviceNotFound
	}
	if err != nil {
		return Device{}, err
	}
	return device, nil
}

type deviceScanner interface {
	Scan(...any) error
}

func scanDevice(row deviceScanner) (Device, error) {
	var device Device
	var mode, scope, network, quality, urls string
	var mirrorDeletions int
	var createdAt, lastSeen sql.NullString
	if err := row.Scan(
		&device.ID, &device.Name, &device.Platform, &device.Build, &mode,
		&scope, &device.Policy.AffinityThreshold, &device.Policy.StorageCapBytes,
		&network, &mirrorDeletions, &quality, &urls, &createdAt, &lastSeen,
	); err != nil {
		return Device{}, err
	}
	device.SyncMode = SyncMode(mode)
	device.Policy = Policy{
		Scope:             Scope(scope),
		AffinityThreshold: device.Policy.AffinityThreshold,
		StorageCapBytes:   device.Policy.StorageCapBytes,
		NetworkRule:       NetworkRule(network),
		MirrorDeletions:   mirrorDeletions != 0,
		Quality:           Quality(quality),
	}
	if strings.TrimSpace(urls) != "" {
		if err := json.Unmarshal([]byte(urls), &device.URLs); err != nil {
			return Device{}, fmt.Errorf("devices: decode urls: %w", err)
		}
	}
	device.CreatedAt = parseTime(createdAt)
	device.LastSeen = parseTime(lastSeen)
	return device, nil
}

// SetSyncMode updates the per-device sync mode.
func (r Repo) SetSyncMode(ctx context.Context, id string, mode SyncMode) error {
	if r.DB == nil {
		return errors.New("devices: database is required")
	}
	if !validMode(mode) {
		return fmt.Errorf("%w: unsupported sync mode %q", ErrInvalidDevice, mode)
	}
	res, err := r.DB.ExecContext(ctx, `UPDATE device SET sync_mode = ? WHERE id = ?`, string(mode), id)
	if err != nil {
		return fmt.Errorf("devices: set sync mode for %s: %w", id, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("devices: set sync mode for %s: %w", id, err)
	}
	if affected == 0 {
		return ErrDeviceNotFound
	}
	return nil
}

// SetURLs replaces the ordered server list a device tries.
func (r Repo) SetURLs(ctx context.Context, id string, urls []string) error {
	if r.DB == nil {
		return errors.New("devices: database is required")
	}
	normalized, err := normalizeURLs(urls)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("devices: encode urls: %w", err)
	}
	res, err := r.DB.ExecContext(ctx, `UPDATE device SET urls = ? WHERE id = ?`, string(encoded), id)
	if err != nil {
		return fmt.Errorf("devices: set urls for %s: %w", id, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("devices: set urls for %s: %w", id, err)
	}
	if affected == 0 {
		return ErrDeviceNotFound
	}
	return nil
}

// SetPolicy replaces one device's transfer policy.
func (r Repo) SetPolicy(ctx context.Context, id string, policy Policy) error {
	if r.DB == nil {
		return errors.New("devices: database is required")
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("devices: begin policy tx: %w", err)
	}
	defer tx.Rollback()

	var mode string
	err = tx.QueryRowContext(ctx, `SELECT sync_mode FROM device WHERE id = ?`, id).Scan(&mode)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDeviceNotFound
	}
	if err != nil {
		return fmt.Errorf("devices: load mode for %s: %w", id, err)
	}
	normalized, err := normalizePolicy(policy, SyncMode(mode))
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE device
SET scope = ?, affinity_threshold = ?, storage_cap_bytes = ?, network_rule = ?, mirror_deletions = ?, quality = ?
WHERE id = ?
`, string(normalized.Scope), normalized.AffinityThreshold, normalized.StorageCapBytes, string(normalized.NetworkRule), boolInt(normalized.MirrorDeletions), string(normalized.Quality), id); err != nil {
		return fmt.Errorf("devices: set policy for %s: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("devices: commit policy tx: %w", err)
	}
	return nil
}

// Touch records that a device was seen.
func (r Repo) Touch(ctx context.Context, id string) error {
	if r.DB == nil {
		return errors.New("devices: database is required")
	}
	res, err := r.DB.ExecContext(ctx, `UPDATE device SET last_seen = ? WHERE id = ?`, r.now().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("devices: touch %s: %w", id, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("devices: touch %s: %w", id, err)
	}
	if affected == 0 {
		return ErrDeviceNotFound
	}
	return nil
}

// Delete removes a device and its holdings.
func (r Repo) Delete(ctx context.Context, id string) error {
	if r.DB == nil {
		return errors.New("devices: database is required")
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("devices: begin delete tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM device_holding WHERE device_id = ?`, id); err != nil {
		return fmt.Errorf("devices: delete holdings for %s: %w", id, err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM device WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("devices: delete %s: %w", id, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("devices: delete %s: %w", id, err)
	}
	if affected == 0 {
		return ErrDeviceNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("devices: commit delete tx: %w", err)
	}
	return nil
}

// ReportHoldings replaces the recorded holdings of a device from a full index
// of its library. Files that are gone from the index stop being recorded, which
// reflects the device rather than asking for a deletion.
func (r Repo) ReportHoldings(ctx context.Context, deviceID string, holdings []Holding) error {
	if r.DB == nil {
		return errors.New("devices: database is required")
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return fmt.Errorf("%w: device id is required", ErrInvalidDevice)
	}
	now := r.now()
	cleaned := make([]Holding, 0, len(holdings))
	seen := make(map[string]struct{}, len(holdings))
	for _, holding := range holdings {
		canonicalID := strings.TrimSpace(holding.CanonicalID)
		if canonicalID == "" {
			return fmt.Errorf("%w: canonical id is required", ErrInvalidHolding)
		}
		hash := strings.ToLower(strings.TrimSpace(holding.Hash))
		if !validHash(hash) {
			return fmt.Errorf("%w: %q is not a sha256 hex digest", ErrInvalidHolding, holding.Hash)
		}
		key := canonicalID + "\x00" + hash
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, Holding{CanonicalID: canonicalID, Hash: hash, LastSeen: now})
	}

	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("devices: begin holdings tx: %w", err)
	}
	defer tx.Rollback()

	var exists int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM device WHERE id = ?`, deviceID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDeviceNotFound
	}
	if err != nil {
		return fmt.Errorf("devices: load device %s: %w", deviceID, err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM device_holding WHERE device_id = ?`, deviceID); err != nil {
		return fmt.Errorf("devices: clear holdings for %s: %w", deviceID, err)
	}
	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO device_holding (device_id, canonical_id, hash, last_seen)
VALUES (?, ?, ?, ?)
`)
	if err != nil {
		return fmt.Errorf("devices: prepare holdings for %s: %w", deviceID, err)
	}
	defer stmt.Close()

	stamp := now.Format(time.RFC3339Nano)
	for _, holding := range cleaned {
		if _, err := stmt.ExecContext(ctx, deviceID, holding.CanonicalID, holding.Hash, stamp); err != nil {
			return fmt.Errorf("devices: store holding %s for %s: %w", holding.CanonicalID, deviceID, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE device SET last_seen = ? WHERE id = ?`, stamp, deviceID); err != nil {
		return fmt.Errorf("devices: touch %s: %w", deviceID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("devices: commit holdings tx: %w", err)
	}
	return nil
}

// ListHoldings returns the recorded files of one device.
func (r Repo) ListHoldings(ctx context.Context, deviceID string) ([]Holding, error) {
	if r.DB == nil {
		return nil, errors.New("devices: database is required")
	}
	rows, err := r.DB.QueryContext(ctx, `
SELECT canonical_id, hash, last_seen
FROM device_holding
WHERE device_id = ?
ORDER BY canonical_id ASC, hash ASC
`, deviceID)
	if err != nil {
		return nil, fmt.Errorf("devices: list holdings for %s: %w", deviceID, err)
	}
	defer rows.Close()

	var out []Holding
	for rows.Next() {
		var holding Holding
		var lastSeen sql.NullString
		if err := rows.Scan(&holding.CanonicalID, &holding.Hash, &lastSeen); err != nil {
			return nil, fmt.Errorf("devices: scan holding: %w", err)
		}
		holding.LastSeen = parseTime(lastSeen)
		out = append(out, holding)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("devices: iterate holdings: %w", err)
	}
	return out, nil
}

// VerifyToken reports whether a plaintext token matches the stored hash.
func (r Repo) VerifyToken(ctx context.Context, id, token string) (bool, error) {
	if r.DB == nil {
		return false, errors.New("devices: database is required")
	}
	var stored sql.NullString
	err := r.DB.QueryRowContext(ctx, `SELECT token_hash FROM device WHERE id = ?`, id).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrDeviceNotFound
	}
	if err != nil {
		return false, fmt.Errorf("devices: load token for %s: %w", id, err)
	}
	if !stored.Valid || stored.String == "" || token == "" {
		return false, nil
	}
	got := hashToken(token)
	return subtle.ConstantTimeCompare([]byte(got), []byte(stored.String)) == 1, nil
}

func (r Repo) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func (r Repo) token() (string, error) {
	if r.Token != nil {
		return r.Token()
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("devices: generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func validHash(hash string) bool {
	if len(hash) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(hash)
	return err == nil
}

func validMode(mode SyncMode) bool {
	switch mode {
	case ModeApp, ModeExternal, ModeOff:
		return true
	default:
		return false
	}
}

func normalizeURLs(urls []string) ([]string, error) {
	out := make([]string, 0, len(urls))
	seen := make(map[string]bool, len(urls))
	for _, raw := range urls {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return nil, fmt.Errorf("%w: invalid server url %q", ErrInvalidDevice, raw)
		}
		clean := strings.TrimRight(raw, "/")
		if seen[clean] {
			continue
		}
		seen[clean] = true
		out = append(out, clean)
	}
	return out, nil
}

func normalizePolicy(policy Policy, mode SyncMode) (Policy, error) {
	if policy.Scope == "" {
		policy.Scope = ScopeEverything
	}
	switch policy.Scope {
	case ScopeEverything, ScopeLiked, ScopeAffinity, ScopePlaylists, ScopeOff:
	default:
		return Policy{}, fmt.Errorf("%w: unsupported scope %q", ErrInvalidDevice, policy.Scope)
	}
	if policy.AffinityThreshold < 0 || policy.AffinityThreshold > 1 {
		return Policy{}, fmt.Errorf("%w: affinity threshold must be between 0 and 1", ErrInvalidDevice)
	}
	if policy.Scope == ScopeAffinity && policy.AffinityThreshold <= 0 {
		return Policy{}, fmt.Errorf("%w: affinity scope needs a threshold", ErrInvalidDevice)
	}
	if policy.StorageCapBytes < 0 {
		return Policy{}, fmt.Errorf("%w: storage cap must not be negative", ErrInvalidDevice)
	}
	if policy.NetworkRule == "" {
		policy.NetworkRule = NetworkAny
	}
	switch policy.NetworkRule {
	case NetworkAny, NetworkWiFi:
	default:
		return Policy{}, fmt.Errorf("%w: unsupported network rule %q", ErrInvalidDevice, policy.NetworkRule)
	}
	if policy.Quality == "" {
		policy.Quality = QualityOriginal
	}
	switch policy.Quality {
	case QualityOriginal, QualityHigh, QualityDataSaver:
	default:
		return Policy{}, fmt.Errorf("%w: unsupported quality %q", ErrInvalidDevice, policy.Quality)
	}
	// An external tool owns the folder, so syncd never asks for deletions
	// there; a Syncthing eviction would otherwise propagate to every peer.
	if mode == ModeExternal && policy.MirrorDeletions {
		return Policy{}, fmt.Errorf("%w: external devices never mirror deletions", ErrInvalidDevice)
	}
	return policy, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// ValidSyncMode reports whether mode is one of the supported sync modes.
func ValidSyncMode(mode SyncMode) bool {
	return validMode(mode)
}

func parseTime(value sql.NullString) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	ts, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return time.Time{}
	}
	return ts
}

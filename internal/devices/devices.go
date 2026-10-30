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
	"errors"
	"fmt"
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

// Device is one registered client.
type Device struct {
	ID        string
	Name      string
	Platform  string
	Build     int
	SyncMode  SyncMode
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

	token, err := r.token()
	if err != nil {
		return Registration{}, err
	}
	now := r.now()
	if _, err := r.DB.ExecContext(ctx, `
INSERT INTO device (id, name, platform, build, last_seen, token_hash, sync_mode)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    platform = excluded.platform,
    build = excluded.build,
    last_seen = excluded.last_seen,
    token_hash = excluded.token_hash,
    sync_mode = excluded.sync_mode
`, device.ID, device.Name, device.Platform, device.Build, now.Format(time.RFC3339Nano), hashToken(token), string(device.SyncMode)); err != nil {
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
SELECT id, name, platform, build, sync_mode, created_at, last_seen
FROM device
ORDER BY id ASC
`)
	if err != nil {
		return nil, fmt.Errorf("devices: list: %w", err)
	}
	defer rows.Close()

	var out []Device
	for rows.Next() {
		var device Device
		var mode string
		var createdAt, lastSeen sql.NullString
		if err := rows.Scan(&device.ID, &device.Name, &device.Platform, &device.Build, &mode, &createdAt, &lastSeen); err != nil {
			return nil, fmt.Errorf("devices: scan: %w", err)
		}
		device.SyncMode = SyncMode(mode)
		device.CreatedAt = parseTime(createdAt)
		device.LastSeen = parseTime(lastSeen)
		out = append(out, device)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("devices: iterate: %w", err)
	}
	return out, nil
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

func validMode(mode SyncMode) bool {
	switch mode {
	case ModeApp, ModeExternal, ModeOff:
		return true
	default:
		return false
	}
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

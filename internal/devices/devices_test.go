package devices

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/internal/store"
	_ "modernc.org/sqlite"
)

func TestRegister(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		device   Device
		wantMode SyncMode
		wantErr  bool
	}{
		{name: "defaults to app mode", device: Device{ID: "phone", Name: "Phone"}, wantMode: ModeApp},
		{name: "external mode", device: Device{ID: "desktop", Name: "Desktop", Platform: "linux", Build: 42, SyncMode: ModeExternal}, wantMode: ModeExternal},
		{name: "off mode", device: Device{ID: "tablet", Name: "Tablet", SyncMode: ModeOff}, wantMode: ModeOff},
		{name: "missing id", device: Device{Name: "Phone"}, wantErr: true},
		{name: "missing name", device: Device{ID: "phone"}, wantErr: true},
		{name: "negative build", device: Device{ID: "phone", Name: "Phone", Build: -1}, wantErr: true},
		{name: "unknown mode", device: Device{ID: "phone", Name: "Phone", SyncMode: "sometimes"}, wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := openDeviceDB(t)
			defer db.Close()

			ctx := context.Background()
			repo := Repo{DB: db, Now: func() time.Time { return now }, Token: fixedToken("secret")}
			registration, err := repo.Register(ctx, tc.device)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidDevice) {
					t.Fatalf("Register() error = %v, want ErrInvalidDevice", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			if registration.Token != "secret" {
				t.Fatalf("token = %q, want secret", registration.Token)
			}
			if registration.Device.SyncMode != tc.wantMode {
				t.Fatalf("sync mode = %q, want %q", registration.Device.SyncMode, tc.wantMode)
			}

			var stored string
			if err := db.QueryRowContext(ctx, `SELECT token_hash FROM device WHERE id = ?`, tc.device.ID).Scan(&stored); err != nil {
				t.Fatalf("load token hash: %v", err)
			}
			if stored == "secret" || stored != hashToken("secret") {
				t.Fatalf("token hash = %q, want hashed secret", stored)
			}

			devices, err := repo.List(ctx)
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if len(devices) != 1 {
				t.Fatalf("devices = %#v, want one", devices)
			}
			if devices[0].ID != tc.device.ID || devices[0].SyncMode != tc.wantMode {
				t.Fatalf("device = %#v, want %s in %q mode", devices[0], tc.device.ID, tc.wantMode)
			}
			if !devices[0].LastSeen.Equal(now) {
				t.Fatalf("last seen = %v, want %v", devices[0].LastSeen, now)
			}
		})
	}
}

func TestRegisterRotatesToken(t *testing.T) {
	t.Parallel()

	db := openDeviceDB(t)
	defer db.Close()
	ctx := context.Background()
	tokens := []string{"old-token", "new-token"}
	issued := 0
	repo := Repo{DB: db, Token: func() (string, error) {
		if issued >= len(tokens) {
			return tokens[len(tokens)-1], nil
		}
		token := tokens[issued]
		issued++
		return token, nil
	}}
	if _, err := repo.Register(ctx, Device{ID: "phone", Name: "Phone"}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	ok, err := repo.VerifyToken(ctx, "phone", "old-token")
	if err != nil {
		t.Fatalf("VerifyToken() error = %v", err)
	}
	if !ok {
		t.Fatal("VerifyToken(old) = false, want true before rotation")
	}

	if _, err := repo.Register(ctx, Device{ID: "phone", Name: "Phone"}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	ok, err = repo.VerifyToken(ctx, "phone", "old-token")
	if err != nil {
		t.Fatalf("VerifyToken() error = %v", err)
	}
	if ok {
		t.Fatal("VerifyToken(old) = true, want false after rotation")
	}
	ok, err = repo.VerifyToken(ctx, "phone", "new-token")
	if err != nil {
		t.Fatalf("VerifyToken() error = %v", err)
	}
	if !ok {
		t.Fatal("VerifyToken(new) = false, want true")
	}
	if ok, err := repo.VerifyToken(ctx, "phone", ""); err != nil || ok {
		t.Fatalf("VerifyToken(empty) = %v, %v, want false, nil", ok, err)
	}
	if _, err := repo.VerifyToken(ctx, "missing", "new-token"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("VerifyToken(missing) error = %v, want ErrDeviceNotFound", err)
	}
}

func TestSyncModeAndTouch(t *testing.T) {
	t.Parallel()

	db := openDeviceDB(t)
	defer db.Close()
	ctx := context.Background()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	repo := Repo{DB: db, Now: func() time.Time { return now }, Token: fixedToken("token")}
	if _, err := repo.Register(ctx, Device{ID: "phone", Name: "Phone"}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if err := repo.SetSyncMode(ctx, "phone", ModeExternal); err != nil {
		t.Fatalf("SetSyncMode() error = %v", err)
	}
	if err := repo.SetSyncMode(ctx, "phone", "sometimes"); !errors.Is(err, ErrInvalidDevice) {
		t.Fatalf("SetSyncMode(invalid) error = %v, want ErrInvalidDevice", err)
	}
	if err := repo.SetSyncMode(ctx, "missing", ModeApp); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("SetSyncMode(missing) error = %v, want ErrDeviceNotFound", err)
	}

	now = now.Add(time.Hour)
	if err := repo.Touch(ctx, "phone"); err != nil {
		t.Fatalf("Touch() error = %v", err)
	}
	if err := repo.Touch(ctx, "missing"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("Touch(missing) error = %v, want ErrDeviceNotFound", err)
	}

	devices, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if devices[0].SyncMode != ModeExternal {
		t.Fatalf("sync mode = %q, want external", devices[0].SyncMode)
	}
	if !devices[0].LastSeen.Equal(now) {
		t.Fatalf("last seen = %v, want %v", devices[0].LastSeen, now)
	}
}

func TestDeviceURLs(t *testing.T) {
	t.Parallel()

	db := openDeviceDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := Repo{DB: db, Now: time.Now, Token: fixedToken("secret")}

	registration, err := repo.Register(ctx, Device{
		ID:   "phone",
		Name: "Phone",
		URLs: []string{"http://192.168.1.10:8080/", "https://box.tailnet.ts.net", "http://192.168.1.10:8080"},
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	want := []string{"http://192.168.1.10:8080", "https://box.tailnet.ts.net"}
	if !equalStrings(registration.Device.URLs, want) {
		t.Fatalf("registered urls = %#v, want %#v", registration.Device.URLs, want)
	}

	device, err := repo.Get(ctx, "phone")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !equalStrings(device.URLs, want) {
		t.Fatalf("stored urls = %#v, want %#v", device.URLs, want)
	}

	updated := []string{"https://music.example.org"}
	if err := repo.SetURLs(ctx, "phone", updated); err != nil {
		t.Fatalf("SetURLs() error = %v", err)
	}
	device, err = repo.Get(ctx, "phone")
	if err != nil {
		t.Fatalf("Get() after update error = %v", err)
	}
	if !equalStrings(device.URLs, updated) {
		t.Fatalf("updated urls = %#v, want %#v", device.URLs, updated)
	}

	if err := repo.SetURLs(ctx, "phone", []string{"ftp://example.org"}); !errors.Is(err, ErrInvalidDevice) {
		t.Fatalf("SetURLs() invalid error = %v, want ErrInvalidDevice", err)
	}
	if err := repo.SetURLs(ctx, "missing", updated); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("SetURLs() missing error = %v, want ErrDeviceNotFound", err)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestRegisterDefaultsPolicy(t *testing.T) {
	t.Parallel()

	db := openDeviceDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := Repo{DB: db, Token: fixedToken("token")}
	registration, err := repo.Register(ctx, Device{ID: "phone", Name: "Phone"})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	want := Policy{Scope: ScopeEverything, NetworkRule: NetworkAny, Quality: QualityOriginal}
	if registration.Device.Policy != want {
		t.Fatalf("policy = %#v, want %#v", registration.Device.Policy, want)
	}
}

func TestSetPolicy(t *testing.T) {
	t.Parallel()

	db := openDeviceDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := Repo{DB: db, Token: fixedToken("token")}
	if _, err := repo.Register(ctx, Device{ID: "phone", Name: "Phone"}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	want := Policy{
		Scope:             ScopeAffinity,
		AffinityThreshold: 0.6,
		StorageCapBytes:   8 << 30,
		NetworkRule:       NetworkWiFi,
		Quality:           QualityHigh,
	}
	if err := repo.SetPolicy(ctx, "phone", want); err != nil {
		t.Fatalf("SetPolicy() error = %v", err)
	}
	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 1 || list[0].Policy != want {
		t.Fatalf("device = %#v, want policy %#v", list, want)
	}

	if err := repo.SetPolicy(ctx, "missing", want); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("SetPolicy(missing) error = %v, want ErrDeviceNotFound", err)
	}
	if err := repo.SetPolicy(ctx, "phone", Policy{Scope: "sometimes"}); !errors.Is(err, ErrInvalidDevice) {
		t.Fatalf("SetPolicy(invalid) error = %v, want ErrInvalidDevice", err)
	}
}

func TestPolicyValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mode    SyncMode
		policy  Policy
		wantErr bool
	}{
		{name: "defaults", policy: Policy{}},
		{name: "liked scope", policy: Policy{Scope: ScopeLiked}},
		{name: "affinity with threshold", policy: Policy{Scope: ScopeAffinity, AffinityThreshold: 0.5}},
		{name: "affinity without threshold", policy: Policy{Scope: ScopeAffinity}, wantErr: true},
		{name: "threshold above one", policy: Policy{AffinityThreshold: 1.5}, wantErr: true},
		{name: "negative threshold", policy: Policy{AffinityThreshold: -0.1}, wantErr: true},
		{name: "negative cap", policy: Policy{StorageCapBytes: -1}, wantErr: true},
		{name: "unknown scope", policy: Policy{Scope: "sometimes"}, wantErr: true},
		{name: "unknown network", policy: Policy{NetworkRule: "carrier_pigeon"}, wantErr: true},
		{name: "unknown quality", policy: Policy{Quality: "lossless"}, wantErr: true},
		{name: "external mirror deletions", mode: ModeExternal, policy: Policy{MirrorDeletions: true}, wantErr: true},
		{name: "app mirror deletions", mode: ModeApp, policy: Policy{MirrorDeletions: true}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizePolicy(tc.policy, tc.mode)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidDevice) {
					t.Fatalf("normalizePolicy() error = %v, want ErrInvalidDevice", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizePolicy() error = %v", err)
			}
			if got.Scope == "" || got.NetworkRule == "" || got.Quality == "" {
				t.Fatalf("policy = %#v, want defaults filled in", got)
			}
		})
	}
}

func TestDeleteRemovesHoldings(t *testing.T) {
	t.Parallel()

	db := openDeviceDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := Repo{DB: db, Token: fixedToken("token")}
	if _, err := repo.Register(ctx, Device{ID: "phone", Name: "Phone"}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-1', 'Song', 'Artist', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO device_holding (device_id, canonical_id, hash) VALUES ('phone', 'track-1', 'abc')`); err != nil {
		t.Fatalf("insert holding: %v", err)
	}

	if err := repo.Delete(ctx, "phone"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := repo.Delete(ctx, "phone"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("Delete() second error = %v, want ErrDeviceNotFound", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_holding WHERE device_id = 'phone'`).Scan(&count); err != nil {
		t.Fatalf("count holdings: %v", err)
	}
	if count != 0 {
		t.Fatalf("holdings = %d, want 0", count)
	}
}

func TestReportHoldings(t *testing.T) {
	t.Parallel()

	validHash := strings.Repeat("ab", 32)
	tests := []struct {
		name     string
		deviceID string
		holdings []Holding
		wantErr  error
	}{
		{
			name:     "stores a hashed file",
			deviceID: "phone",
			holdings: []Holding{{CanonicalID: "track-1", Hash: validHash}},
		},
		{
			name:     "accepts an upper-case digest",
			deviceID: "phone",
			holdings: []Holding{{CanonicalID: "track-1", Hash: strings.ToUpper(validHash)}},
		},
		{
			name:     "deduplicates repeated files",
			deviceID: "phone",
			holdings: []Holding{{CanonicalID: "track-1", Hash: validHash}, {CanonicalID: "track-1", Hash: validHash}},
		},
		{
			name:     "rejects a short digest",
			deviceID: "phone",
			holdings: []Holding{{CanonicalID: "track-1", Hash: "abc"}},
			wantErr:  ErrInvalidHolding,
		},
		{
			name:     "rejects a missing track",
			deviceID: "phone",
			holdings: []Holding{{CanonicalID: "", Hash: validHash}},
			wantErr:  ErrInvalidHolding,
		},
		{
			name:     "rejects an unknown device",
			deviceID: "missing",
			holdings: []Holding{{CanonicalID: "track-1", Hash: validHash}},
			wantErr:  ErrDeviceNotFound,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := openDeviceDB(t)
			defer db.Close()
			ctx := context.Background()
			now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
			repo := Repo{DB: db, Now: func() time.Time { return now }, Token: fixedToken("token")}
			if _, err := repo.Register(ctx, Device{ID: "phone", Name: "Phone"}); err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-1', 'Song', 'Artist', 180000)`); err != nil {
				t.Fatalf("insert track: %v", err)
			}

			err := repo.ReportHoldings(ctx, tc.deviceID, tc.holdings)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("ReportHoldings() error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReportHoldings() error = %v", err)
			}

			holdings, err := repo.ListHoldings(ctx, "phone")
			if err != nil {
				t.Fatalf("ListHoldings() error = %v", err)
			}
			if len(holdings) != 1 {
				t.Fatalf("holdings = %#v, want one", holdings)
			}
			if holdings[0].Hash != validHash {
				t.Fatalf("hash = %q, want %q", holdings[0].Hash, validHash)
			}
			if !holdings[0].LastSeen.Equal(now) {
				t.Fatalf("last seen = %v, want %v", holdings[0].LastSeen, now)
			}
		})
	}
}

func TestReportHoldingsReplacesPreviousSet(t *testing.T) {
	t.Parallel()

	db := openDeviceDB(t)
	defer db.Close()
	ctx := context.Background()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	repo := Repo{DB: db, Now: func() time.Time { return now }, Token: fixedToken("token")}
	if _, err := repo.Register(ctx, Device{ID: "phone", Name: "Phone"}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	for _, id := range []string{"track-1", "track-2"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES (?, 'Song', 'Artist', 180000)`, id); err != nil {
			t.Fatalf("insert track %s: %v", id, err)
		}
	}

	first := strings.Repeat("ab", 32)
	second := strings.Repeat("cd", 32)
	if err := repo.ReportHoldings(ctx, "phone", []Holding{
		{CanonicalID: "track-1", Hash: first},
		{CanonicalID: "track-2", Hash: second},
	}); err != nil {
		t.Fatalf("ReportHoldings() error = %v", err)
	}
	if err := repo.ReportHoldings(ctx, "phone", []Holding{{CanonicalID: "track-2", Hash: second}}); err != nil {
		t.Fatalf("ReportHoldings() error = %v", err)
	}

	holdings, err := repo.ListHoldings(ctx, "phone")
	if err != nil {
		t.Fatalf("ListHoldings() error = %v", err)
	}
	if len(holdings) != 1 || holdings[0].CanonicalID != "track-2" {
		t.Fatalf("holdings = %#v, want only track-2", holdings)
	}
}

func fixedToken(token string) func() (string, error) {
	return func() (string, error) { return token, nil }
}

func openDeviceDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "syncd-devices.db")
	db, err := sql.Open("sqlite", store.DSNForTest(path))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	return db
}

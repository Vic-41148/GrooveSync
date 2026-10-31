// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package devices

import (
	"context"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestRegisterSyncModeTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		seed     SyncMode
		register SyncMode
		want     SyncMode
	}{
		{name: "new device without mode defaults to app", register: "", want: ModeApp},
		{name: "new device with explicit external mode", register: ModeExternal, want: ModeExternal},
		{name: "re-register without mode keeps existing", seed: ModeExternal, register: "", want: ModeExternal},
		{name: "re-register with explicit mode changes it", seed: ModeExternal, register: ModeApp, want: ModeApp},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := openDeviceDB(t)
			defer db.Close()
			ctx := context.Background()
			repo := Repo{DB: db, Now: func() time.Time { return time.Now().UTC() }, Token: fixedToken("token")}

			if tc.seed != "" {
				if _, err := repo.Register(ctx, Device{ID: "phone", Name: "Phone", SyncMode: tc.seed}); err != nil {
					t.Fatalf("seed Register() error = %v", err)
				}
			}
			if _, err := repo.Register(ctx, Device{ID: "phone", Name: "Phone", SyncMode: tc.register}); err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			got, err := repo.Get(ctx, "phone")
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if got.SyncMode != tc.want {
				t.Fatalf("sync mode = %q, want %q", got.SyncMode, tc.want)
			}
		})
	}
}

func TestRegisterReportsStoredModeOnReRegister(t *testing.T) {
	t.Parallel()

	db := openDeviceDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := Repo{DB: db, Now: func() time.Time { return time.Now().UTC() }, Token: fixedToken("token")}

	if _, err := repo.Register(ctx, Device{ID: "phone", Name: "Phone", SyncMode: ModeExternal}); err != nil {
		t.Fatalf("seed Register() error = %v", err)
	}

	reg, err := repo.Register(ctx, Device{ID: "phone", Name: "Phone"})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if reg.Device.SyncMode != ModeExternal {
		t.Fatalf("response sync mode = %q, want %q", reg.Device.SyncMode, ModeExternal)
	}
	got, err := repo.Get(ctx, "phone")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.SyncMode != ModeExternal {
		t.Fatalf("stored sync mode = %q, want %q", got.SyncMode, ModeExternal)
	}
	if got.Policy != reg.Device.Policy {
		t.Fatalf("stored policy = %+v, response policy = %+v", got.Policy, reg.Device.Policy)
	}

	// An external device never mirrors deletions. If an omitted mode were
	// normalised as app this would be accepted and stored.
	if _, err := repo.Register(ctx, Device{ID: "phone", Name: "Phone", Policy: Policy{MirrorDeletions: true}}); !errors.Is(err, ErrInvalidDevice) {
		t.Fatalf("Register(mirror deletions) error = %v, want ErrInvalidDevice", err)
	}
}

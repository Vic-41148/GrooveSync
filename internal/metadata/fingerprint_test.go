package metadata

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFPCalcFingerprint(t *testing.T) {
	t.Parallel()

	binary := filepath.Join(t.TempDir(), "fpcalc")
	script := "#!/bin/sh\nprintf '%s' '{\"duration\": 90.5, \"fingerprint\": \"FP\"}'\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write fpcalc stub: %v", err)
	}

	got, err := FPCalc{Path: binary}.Fingerprint(context.Background(), "/music/song.flac")
	if err != nil {
		t.Fatalf("Fingerprint() error = %v", err)
	}
	if got.Value != "FP" || got.Duration != 90 {
		t.Fatalf("fingerprint = %#v, want FP and 90 seconds", got)
	}
}

func TestFPCalcFingerprintErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		script string
		path   string
	}{
		{name: "missing path"},
		{name: "empty output", script: "#!/bin/sh\nexit 0\n", path: "/music/song.flac"},
		{name: "failure", script: "#!/bin/sh\necho broken >&2\nexit 1\n", path: "/music/song.flac"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			binary := filepath.Join(t.TempDir(), "fpcalc")
			if tc.script == "" {
				tc.script = "#!/bin/sh\nexit 0\n"
			}
			if err := os.WriteFile(binary, []byte(tc.script), 0o755); err != nil {
				t.Fatalf("write fpcalc stub: %v", err)
			}
			if _, err := (FPCalc{Path: binary}).Fingerprint(context.Background(), tc.path); err == nil {
				t.Fatal("Fingerprint() error = nil, want failure")
			}
		})
	}
}

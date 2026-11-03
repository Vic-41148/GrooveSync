// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package acquisition

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoreContainsNoExtractionCode(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	forbidden := []string{"yt-dlp", "youtube-dl", "googlevideo", "pytube", "yt_dlp"}
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := info.Name()
			if base == ".git" || base == "spikes" || base == "temp" || base == "vendor" || base == "build" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if filepath.Base(path) == "guard_test.go" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lower := strings.ToLower(string(data))
		for _, term := range forbidden {
			if strings.Contains(lower, term) {
				t.Errorf("extraction term %q found in %s", term, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
}

func TestCoreDoesNotDependOnPlugin(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if strings.Contains(strings.ToLower(string(data)), "provisioner") {
		t.Fatal("core go.mod must not depend on the acquisition plugin")
	}
}

// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

// Package docs guards the local planning files and the project's own prose.
// It skips when docs/todo is absent, because docs/ is not committed.
package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

func TestPhase6NotStartedHasNoTicks(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "docs", "todo", "phase-6.md")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		t.Skip("docs/todo is local only and absent from this checkout")
	}
	if err != nil {
		t.Fatalf("read phase 6: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "Status: not started") {
		t.Skip("phase 6 is no longer marked not started")
	}
	for i, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- [x]") {
			t.Errorf("phase-6.md:%d ticks an item while the header says not started", i+1)
		}
	}
}

func TestAuthoredTextHasNoEmDashOrEmoji(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "docs", "todo")); err != nil {
		t.Skip("docs/todo is local only and absent from this checkout")
	}

	paths := []string{
		"README.md", "CONTRIBUTING.md", "AGENTS.md", "SECURITY.md",
		"AI-POLICY.md", "THIRD_PARTY_NOTES.md",
		"docs/todo", "docs/audit",
		"server", "common", "desktop", "android", "tests", "tools",
	}
	for _, rel := range paths {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if err := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				case ".git", "node_modules", "build", ".gradle":
					return filepath.SkipDir
				}
				return nil
			}
			if !authoredText(p) {
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(data), "\n") {
				if strings.ContainsRune(line, '\u2014') {
					t.Errorf("%s:%d contains an em dash", p, i+1)
				}
				for _, r := range line {
					if isEmoji(r) {
						t.Errorf("%s:%d contains an emoji", p, i+1)
						break
					}
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("walk %s: %v", rel, err)
		}
	}
}

func authoredText(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".go", ".kt", ".kts", ".yml", ".yaml", ".json", ".sh", ".toml":
		return true
	default:
		return false
	}
}

func isEmoji(r rune) bool {
	return (r >= 0x1F300 && r <= 0x1FAFF) || (r >= 0x2600 && r <= 0x27BF)
}

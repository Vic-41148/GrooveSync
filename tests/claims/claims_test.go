// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

// Package claims keeps the completion claims in the planning documents tied to
// evidence in the repository. It is deliberately small: the manifest is the
// source of truth, this file only checks that every piece of evidence named in
// the manifest still exists.
package claims

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// allowedStatuses is the vocabulary used by docs/audit and tests/claims. A typo
// in the manifest must fail the test rather than silently disable a claim.
var allowedStatuses = map[string]bool{
	"verified":     true,
	"false":        true,
	"overstated":   true,
	"stale":        true,
	"unverifiable": true,
	"open":         true,
}

type manifest struct {
	Version int     `json:"version"`
	Claims  []claim `json:"claims"`
}

type claim struct {
	ID       string   `json:"id"`
	Doc      string   `json:"doc"`
	Phase    string   `json:"phase"`
	Claim    string   `json:"claim"`
	Status   string   `json:"status"`
	Severity string   `json:"severity"`
	Evidence []string `json:"evidence"`
	Note     string   `json:"note"`
}

func TestClaimsMatchReality(t *testing.T) {
	root := repoRoot(t)
	m := loadManifest(t, root)

	if m.Version != 1 {
		t.Fatalf("manifest version = %d, want 1", m.Version)
	}
	if len(m.Claims) == 0 {
		t.Fatal("manifest has no claims")
	}

	seen := make(map[string]bool, len(m.Claims))
	previous := ""
	for _, c := range m.Claims {
		if c.ID == "" {
			t.Fatalf("claim with empty id: %+v", c)
		}
		if seen[c.ID] {
			t.Fatalf("duplicate claim id %q", c.ID)
		}
		if c.ID < previous {
			t.Fatalf("claims are not sorted: %q comes after %q", c.ID, previous)
		}
		seen[c.ID] = true
		previous = c.ID

		if !allowedStatuses[c.Status] {
			t.Fatalf("%s: unknown status %q", c.ID, c.Status)
		}
		if strings.TrimSpace(c.Claim) == "" {
			t.Fatalf("%s: empty claim text", c.ID)
		}
		if len(c.Evidence) == 0 && c.Status != "unverifiable" {
			t.Fatalf("%s: status %q requires at least one evidence entry", c.ID, c.Status)
		}
	}

	for _, c := range m.Claims {
		c := c
		t.Run(c.ID, func(t *testing.T) {
			if len(c.Evidence) == 0 {
				t.Skip("unverifiable, no repository evidence")
			}
			for _, ev := range c.Evidence {
				if err := checkEvidence(root, ev); err != nil {
					t.Errorf("evidence %q: %v", ev, err)
				}
			}
		})
	}
}

func TestManifestCoversEveryPhaseDocument(t *testing.T) {
	root := repoRoot(t)
	todo := filepath.Join(root, "docs", "todo")
	if _, err := os.Stat(todo); err != nil {
		t.Skip("docs/todo is local only and absent from this checkout")
	}

	entries, err := os.ReadDir(todo)
	if err != nil {
		t.Fatalf("read docs/todo: %v", err)
	}
	m := loadManifest(t, root)
	covered := make(map[string]bool, len(m.Claims))
	for _, c := range m.Claims {
		covered[c.Doc] = true
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "phase-") || !strings.HasSuffix(name, ".md") {
			continue
		}
		doc := filepath.ToSlash(filepath.Join("docs", "todo", name))
		if !covered[doc] {
			t.Errorf("%s has no claim in tests/claims/claims.json", doc)
		}
	}
}

func loadManifest(t *testing.T, root string) manifest {
	t.Helper()
	path := filepath.Join(root, "tests", "claims", "claims.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	return m
}

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

// checkEvidence understands the small grammar documented in tests/README.md.
// Every prefix answers one question about the working tree.
func checkEvidence(root, ev string) error {
	switch {
	case strings.HasPrefix(ev, "file:"):
		return requireFile(root, strings.TrimPrefix(ev, "file:"))
	case strings.HasPrefix(ev, "absent:"):
		return requireAbsent(root, strings.TrimPrefix(ev, "absent:"))
	case strings.HasPrefix(ev, "workflow:"):
		return requireFile(root, filepath.Join(".github", "workflows", strings.TrimPrefix(ev, "workflow:")))
	case strings.HasPrefix(ev, "contains:"):
		path, token, err := splitToken(ev, "contains:")
		if err != nil {
			return err
		}
		return requireToken(root, path, token, true)
	case strings.HasPrefix(ev, "lacks:"):
		path, token, err := splitToken(ev, "lacks:")
		if err != nil {
			return err
		}
		return requireToken(root, path, token, false)
	case strings.HasPrefix(ev, "test:"):
		return requireGoTest(root, strings.TrimPrefix(ev, "test:"))
	case strings.HasPrefix(ev, "nosource:"):
		return requireAbsentInTrees(root, []string{"server"}, ".go", true, strings.TrimPrefix(ev, "nosource:"))
	case strings.HasPrefix(ev, "nokotlin:"):
		return requireAbsentInTrees(root, []string{"android", "desktop", "common"}, ".kt", false, strings.TrimPrefix(ev, "nokotlin:"))
	case strings.HasPrefix(ev, "nodep:"):
		return requireAbsentInDeps(root, strings.TrimPrefix(ev, "nodep:"))
	default:
		return fmt.Errorf("unknown evidence prefix")
	}
}

func splitToken(ev, prefix string) (string, string, error) {
	rest := strings.TrimPrefix(ev, prefix)
	path, token, ok := strings.Cut(rest, "::")
	if !ok || path == "" || token == "" {
		return "", "", fmt.Errorf("want <path>::<token>")
	}
	return path, token, nil
}

func requireFile(root, rel string) error {
	if rel == "" {
		return fmt.Errorf("empty path")
	}
	info, err := os.Stat(filepath.Join(root, rel))
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", rel)
	}
	return nil
}

func requireAbsent(root, rel string) error {
	if rel == "" {
		return fmt.Errorf("empty path")
	}
	if _, err := os.Stat(filepath.Join(root, rel)); err == nil {
		return fmt.Errorf("%s exists", rel)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func requireToken(root, rel, token string, want bool) error {
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return err
	}
	has := strings.Contains(string(data), token)
	if want && !has {
		return fmt.Errorf("%s does not contain %q", rel, token)
	}
	if !want && has {
		return fmt.Errorf("%s unexpectedly contains %q", rel, token)
	}
	return nil
}

func requireGoTest(root, name string) error {
	if name == "" {
		return fmt.Errorf("empty test name")
	}
	needle := "func " + name + "("
	var found string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "build", ".gradle", "spikes", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(data), needle) {
			found = path
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return err
	}
	if found == "" {
		return fmt.Errorf("no Go test named %s", name)
	}
	return nil
}

func requireAbsentInTrees(root string, trees []string, ext string, skipTests bool, token string) error {
	if token == "" {
		return fmt.Errorf("empty token")
	}
	for _, tree := range trees {
		base := filepath.Join(root, tree)
		if _, err := os.Stat(base); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				case ".git", "build", ".gradle", "node_modules":
					return filepath.SkipDir
				}
				return nil
			}
			if filepath.Ext(path) != ext {
				return nil
			}
			if skipTests && strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if strings.Contains(string(data), token) {
				return fmt.Errorf("%s contains %q", path, token)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func requireAbsentInDeps(root, token string) error {
	if token == "" {
		return fmt.Errorf("empty token")
	}
	candidates := []string{
		"go.mod",
		"gradle/libs.versions.toml",
		"android/build.gradle.kts",
		"desktop/build.gradle.kts",
		"common/build.gradle.kts",
		"build.gradle.kts",
	}
	needle := strings.ToLower(token)
	for _, rel := range candidates {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if strings.Contains(strings.ToLower(string(data)), needle) {
			return fmt.Errorf("%s declares %q", rel, token)
		}
	}
	return nil
}

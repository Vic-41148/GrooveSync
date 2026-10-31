// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package filesync

import (
	"reflect"
	"testing"
)

func TestDiff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		local         []File
		remote        []File
		opts          Options
		wantUploads   []string
		wantDownload  []string
		wantLocalDel  []string
		wantRemoteDel []string
		wantConflict  []string
		wantUnchanged int
	}{
		{
			name:          "new local file uploads",
			local:         []File{{Path: "Artist/Album/01.flac", Hash: "a"}},
			wantUploads:   []string{"Artist/Album/01.flac"},
			wantUnchanged: 0,
		},
		{
			name:          "new remote file downloads",
			remote:        []File{{Path: "Artist/Album/02.flac", Hash: "b"}},
			wantDownload:  []string{"Artist/Album/02.flac"},
			wantUnchanged: 0,
		},
		{
			name:          "same path and hash is unchanged",
			local:         []File{{Path: "Artist/Album/01.flac", Hash: "a"}},
			remote:        []File{{Path: "Artist/Album/01.flac", Hash: "a"}},
			wantUnchanged: 1,
		},
		{
			name:          "same content at another path needs no transfer",
			local:         []File{{Path: "Artist/Album/01.flac", Hash: "a"}},
			remote:        []File{{Path: "Other/01.flac", Hash: "a"}},
			wantUnchanged: 1,
		},
		{
			name:          "same path with different hashes conflicts",
			local:         []File{{Path: "Artist/Album/01.flac", Hash: "a"}},
			remote:        []File{{Path: "Artist/Album/01.flac", Hash: "b"}},
			wantConflict:  []string{"Artist/Album/01.flac"},
			wantUnchanged: 0,
		},
		{
			name:          "deletions stay off by default",
			local:         []File{{Path: "Artist/Album/01.flac", Hash: "a"}},
			remote:        []File{{Path: "Artist/Album/02.flac", Hash: "b"}},
			wantUploads:   []string{"Artist/Album/01.flac"},
			wantDownload:  []string{"Artist/Album/02.flac"},
			wantUnchanged: 0,
		},
		{
			name:          "mirror deletions use the baseline",
			local:         []File{{Path: "Artist/Album/01.flac", Hash: "a"}},
			remote:        []File{{Path: "Artist/Album/02.flac", Hash: "b"}},
			opts:          Options{MirrorDeletions: true, Baseline: []File{{Path: "Artist/Album/01.flac", Hash: "a"}, {Path: "Artist/Album/02.flac", Hash: "b"}}},
			wantLocalDel:  []string{"Artist/Album/01.flac"},
			wantRemoteDel: []string{"Artist/Album/02.flac"},
			wantUnchanged: 0,
		},
		{
			name:          "mirror deletions keep shared content",
			local:         []File{{Path: "A/01.flac", Hash: "a"}},
			remote:        []File{{Path: "B/01.flac", Hash: "a"}, {Path: "B/02.flac", Hash: "b"}},
			opts:          Options{MirrorDeletions: true, Baseline: []File{{Path: "A/01.flac", Hash: "a"}, {Path: "B/02.flac", Hash: "b"}}},
			wantRemoteDel: []string{"B/02.flac"},
			wantUnchanged: 1,
		},
		{
			name:          "mirror deletions ignore new files without a baseline",
			local:         []File{{Path: "A/01.flac", Hash: "a"}},
			remote:        []File{{Path: "B/02.flac", Hash: "b"}},
			opts:          Options{MirrorDeletions: true},
			wantUploads:   []string{"A/01.flac"},
			wantDownload:  []string{"B/02.flac"},
			wantUnchanged: 0,
		},
		{
			name:          "conflicts are never deleted",
			local:         []File{{Path: "A/01.flac", Hash: "a"}},
			remote:        []File{{Path: "A/01.flac", Hash: "b"}},
			opts:          Options{MirrorDeletions: true, Baseline: []File{{Path: "A/01.flac", Hash: "a"}}},
			wantConflict:  []string{"A/01.flac"},
			wantUnchanged: 0,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := Diff(tc.local, tc.remote, tc.opts)
			assertPaths(t, "uploads", plan.Uploads, tc.wantUploads)
			assertPaths(t, "downloads", plan.Downloads, tc.wantDownload)
			assertPaths(t, "local deletes", plan.LocalDeletes, tc.wantLocalDel)
			assertPaths(t, "remote deletes", plan.RemoteDeletes, tc.wantRemoteDel)
			conflicts := make([]string, 0, len(plan.Conflicts))
			for _, conflict := range plan.Conflicts {
				conflicts = append(conflicts, conflict.Path)
			}
			if !reflect.DeepEqual(conflicts, nonNil(tc.wantConflict)) {
				t.Fatalf("conflicts = %#v, want %#v", conflicts, tc.wantConflict)
			}
			if plan.Unchanged != tc.wantUnchanged {
				t.Fatalf("unchanged = %d, want %d", plan.Unchanged, tc.wantUnchanged)
			}
		})
	}
}

func TestClassifyPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want PathKind
	}{
		{name: "normal track", path: "Artist/Album/01.flac", want: PathNormal},
		{name: "syncthing folder marker", path: "Music/.stfolder", want: PathIgnored},
		{name: "syncthing versions", path: "Music/.stversions/Artist/01.flac", want: PathIgnored},
		{name: "syncthing temp", path: "Music/.syncthing.01.flac.tmp", want: PathIgnored},
		{name: "conflict copy", path: "Music/Artist/01.sync-conflict-20261030-120000-ABCDEF.flac", want: PathConflictCopy},
		{name: "windows separator", path: "Music\\.stfolder", want: PathIgnored},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ClassifyPath(tc.path); got != tc.want {
				t.Fatalf("ClassifyPath(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func assertPaths(t *testing.T, name string, files []File, want []string) {
	t.Helper()

	got := make([]string, 0, len(files))
	for _, file := range files {
		got = append(got, file.Path)
	}
	if !reflect.DeepEqual(got, nonNil(want)) {
		t.Fatalf("%s = %#v, want %#v", name, got, want)
	}
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

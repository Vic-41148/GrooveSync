// Package filesync plans content-addressed library transfers between devices.
package filesync

import (
	"path"
	"sort"
	"strings"
)

// File is one library entry keyed by its content hash.
type File struct {
	Path string
	Hash string
	Size int64
}

// Options controls deletion behaviour.
type Options struct {
	MirrorDeletions bool
	// Baseline is the last agreed manifest. Deletions require it so a new file
	// is not mistaken for a removed one.
	Baseline []File
}

// Conflict keeps both versions when a path changed on two devices.
type Conflict struct {
	Path   string
	Local  File
	Remote File
}

// Plan lists the transfers and conflicts for one manifest comparison.
type Plan struct {
	Uploads       []File
	Downloads     []File
	LocalDeletes  []File
	RemoteDeletes []File
	Conflicts     []Conflict
	Unchanged     int
}

// PathKind classifies housekeeping paths that must not enter the library.
type PathKind int

const (
	PathNormal PathKind = iota
	PathIgnored
	PathConflictCopy
)

// ClassifyPath reports Syncthing housekeeping and conflict copies.
func ClassifyPath(name string) PathKind {
	normalized := strings.ReplaceAll(name, "\\", "/")
	for _, part := range strings.Split(normalized, "/") {
		if part == ".stfolder" || part == ".stversions" {
			return PathIgnored
		}
	}
	base := path.Base(normalized)
	switch base {
	case ".stignore", ".DS_Store":
		return PathIgnored
	}
	if strings.HasPrefix(base, ".syncthing.") && strings.HasSuffix(base, ".tmp") {
		return PathIgnored
	}
	if strings.Contains(base, ".sync-conflict-") {
		return PathConflictCopy
	}
	return PathNormal
}

// Diff compares two manifests by path and content hash. Deletions are only
// planned when MirrorDeletions is set, because losing a liked track is worse
// than keeping an extra copy.
func Diff(local, remote []File, opts Options) Plan {
	localByPath := indexByPath(local)
	remoteByPath := indexByPath(remote)

	localHashes := make(map[string]struct{}, len(local))
	for _, file := range local {
		if file.Hash != "" {
			localHashes[file.Hash] = struct{}{}
		}
	}
	remoteHashes := make(map[string]struct{}, len(remote))
	for _, file := range remote {
		if file.Hash != "" {
			remoteHashes[file.Hash] = struct{}{}
		}
	}

	var plan Plan
	deleting := make(map[string]struct{})
	if opts.MirrorDeletions {
		for _, base := range opts.Baseline {
			localFile, hasLocal := localByPath[base.Path]
			remoteFile, hasRemote := remoteByPath[base.Path]
			switch {
			case hasLocal && !hasRemote:
				if _, shared := remoteHashes[localFile.Hash]; shared && localFile.Hash != "" {
					continue
				}
				plan.LocalDeletes = append(plan.LocalDeletes, localFile)
				deleting[base.Path] = struct{}{}
			case !hasLocal && hasRemote:
				if _, shared := localHashes[remoteFile.Hash]; shared && remoteFile.Hash != "" {
					continue
				}
				plan.RemoteDeletes = append(plan.RemoteDeletes, remoteFile)
				deleting[base.Path] = struct{}{}
			}
		}
	}

	for _, file := range local {
		if _, skip := deleting[file.Path]; skip {
			continue
		}
		remoteFile, ok := remoteByPath[file.Path]
		_, remoteHasHash := remoteHashes[file.Hash]
		switch {
		case ok && file.Hash == remoteFile.Hash:
			plan.Unchanged++
		case ok:
			plan.Conflicts = append(plan.Conflicts, Conflict{Path: file.Path, Local: file, Remote: remoteFile})
		case remoteHasHash && file.Hash != "":
			plan.Unchanged++
		default:
			plan.Uploads = append(plan.Uploads, file)
		}
	}

	for _, file := range remote {
		if _, skip := deleting[file.Path]; skip {
			continue
		}
		if _, ok := localByPath[file.Path]; ok {
			continue
		}
		if _, present := localHashes[file.Hash]; present && file.Hash != "" {
			continue
		}
		plan.Downloads = append(plan.Downloads, file)
	}

	sortFiles(plan.Uploads)
	sortFiles(plan.Downloads)
	sortFiles(plan.LocalDeletes)
	sortFiles(plan.RemoteDeletes)
	sort.SliceStable(plan.Conflicts, func(i, j int) bool {
		return plan.Conflicts[i].Path < plan.Conflicts[j].Path
	})
	return plan
}

func indexByPath(files []File) map[string]File {
	out := make(map[string]File, len(files))
	for _, file := range files {
		out[file.Path] = file
	}
	return out
}

func sortFiles(files []File) {
	sort.SliceStable(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
}

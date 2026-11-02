// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

//go:build cgo

package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "unsafe"
)

/*
#cgo LDFLAGS: -ltag_c
#include <stdlib.h>
#include <string.h>
#include <taglib/tag_c.h>

static char *scan_replay_gain(TagLib_File *file) {
    char **keys = taglib_property_keys(file);
    if (keys == NULL) return NULL;
    char *out = NULL;
    for (int i = 0; keys[i] != NULL; i++) {
        if (strcasestr(keys[i], "REPLAYGAIN") != NULL && strcasestr(keys[i], "GAIN") != NULL) {
            char **values = taglib_property_get(file, keys[i]);
            if (values != NULL && values[0] != NULL) {
                out = strdup(values[0]);
            }
            if (values != NULL) taglib_property_free(values);
            break;
        }
    }
    taglib_property_free(keys);
    return out;
}

static int has_embedded_art(TagLib_File *file) {
    char **keys = taglib_property_keys(file);
    if (keys == NULL) return 0;
    int found = 0;
    for (int i = 0; keys[i] != NULL; i++) {
        if (strcasecmp(keys[i], "PICTURE") == 0 || strcasestr(keys[i], "COVER") != NULL) {
            found = 1;
            break;
        }
    }
    taglib_property_free(keys);
    return found;
}
*/
import "C"

import "unsafe"

// ScannedTags are the metadata read from one audio file.
type ScannedTags struct {
	Title       string
	Artist      string
	Album       string
	DurationMS  int
	EmbeddedArt bool
	FolderArt   string
	ReplayGain  string
}

// Scanner walks a filesystem root and indexes audio files by content hash,
// reading their tags with TagLib. It reuses the ingest and hash machinery so
// scanned files deduplicate against uploads and Syncthing contributions.
type Scanner struct {
	DB                *sql.DB
	Root              string
	StabilityInterval time.Duration
}

// Scan indexes every regular file under Root. It returns the number of files
// ingested.
func (s Scanner) Scan(ctx context.Context) (int, error) {
	if s.DB == nil {
		return 0, errors.New("library: database is required")
	}
	if strings.TrimSpace(s.Root) == "" {
		return 0, errors.New("library: root is required")
	}
	ing := Ingestor{DB: s.DB}
	hb := HashBackfill{DB: s.DB, Root: s.Root, StabilityInterval: s.StabilityInterval}

	var scanned int
	err := filepath.Walk(s.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info == nil || info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".cue") {
			return nil
		}

		sum, err := hb.hashFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}

		canonicalID, err := ing.IngestWithSource(ctx, "", path, sum, filepath.Base(path), "scan")
		if err != nil {
			return err
		}

		tags, _ := readTagLib(path)
		if strings.HasPrefix(canonicalID, "upload-") && (tags.Title != "" || tags.Artist != "" || tags.Album != "" || tags.DurationMS > 0) {
			artist := splitScannedArtists(tags.Artist)
			if _, err := s.DB.ExecContext(ctx, `
UPDATE track
SET title = COALESCE(NULLIF(?, ''), title),
    artist = COALESCE(NULLIF(?, ''), artist),
    album = COALESCE(NULLIF(?, ''), album),
    duration_ms = CASE WHEN ? > 0 THEN ? ELSE duration_ms END
WHERE canonical_id = ?
`, tags.Title, artist, tags.Album, tags.DurationMS, tags.DurationMS, canonicalID); err != nil {
				return fmt.Errorf("library: update scanned track %s: %w", canonicalID, err)
			}
		}
		scanned++
		return nil
	})
	if err != nil {
		return scanned, err
	}
	return scanned, nil
}

func readTagLib(path string) (ScannedTags, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))

	file := C.taglib_file_new(cpath)
	if file == nil {
		return ScannedTags{}, fmt.Errorf("library: taglib could not open %s", path)
	}
	defer C.taglib_file_free(file)
	if C.taglib_file_is_valid(file) == 0 {
		return ScannedTags{}, fmt.Errorf("library: taglib reports %s as invalid", path)
	}

	var tags ScannedTags
	if tag := C.taglib_file_tag(file); tag != nil {
		tags.Title = strings.TrimSpace(C.GoString(C.taglib_tag_title(tag)))
		tags.Artist = strings.TrimSpace(C.GoString(C.taglib_tag_artist(tag)))
		tags.Album = strings.TrimSpace(C.GoString(C.taglib_tag_album(tag)))
		C.taglib_tag_free_strings()
	}
	if props := C.taglib_file_audioproperties(file); props != nil {
		tags.DurationMS = int(C.taglib_audioproperties_length(props)) * 1000
	}
	tags.EmbeddedArt = C.has_embedded_art(file) != 0
	tags.FolderArt = folderArtPath(path)
	if rg := C.scan_replay_gain(file); rg != nil {
		tags.ReplayGain = C.GoString(rg)
		C.free(unsafe.Pointer(rg))
	}
	return tags, nil
}

func splitScannedArtists(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, sep := range []string{" feat. ", " ft. ", " featuring ", ";", "\\", "/"} {
		value = strings.ReplaceAll(value, sep, ", ")
	}
	parts := strings.Split(value, ",")
	seen := make(map[string]bool)
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || seen[strings.ToLower(part)] {
			continue
		}
		if strings.EqualFold(part, "various artists") {
			part = "Various Artists"
		}
		seen[strings.ToLower(part)] = true
		out = append(out, part)
	}
	return strings.Join(out, ", ")
}

func folderArtPath(audioPath string) string {
	dir := filepath.Dir(audioPath)
	for _, name := range []string{"cover.jpg", "cover.jpeg", "cover.png", "folder.jpg", "folder.png"} {
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

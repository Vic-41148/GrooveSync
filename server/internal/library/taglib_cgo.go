// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

//go:build cgo

package library

import (
	"fmt"
	"strings"
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

// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

//go:build !cgo

package library

import "errors"

// readTagLib is the default-build fallback. TagLib needs cgo, so a build
// without it still indexes files by hash but leaves their tags untouched.
func readTagLib(path string) (ScannedTags, error) {
	return ScannedTags{}, errors.New("library: taglib support is not compiled in")
}

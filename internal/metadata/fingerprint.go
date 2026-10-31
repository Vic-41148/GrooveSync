// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// DefaultFPCalcPath is the Chromaprint executable syncd calls.
const DefaultFPCalcPath = "fpcalc"

// DefaultFingerprintLength is how many seconds of audio fpcalc reads.
const DefaultFingerprintLength = 120

// FPCalc computes Chromaprint fingerprints by running the fpcalc executable.
// Chromaprint ships as a separate binary so its LGPL parts stay out of the
// process.
type FPCalc struct {
	Path   string
	Length int
}

// Fingerprint reads one file and returns its fingerprint.
func (f FPCalc) Fingerprint(ctx context.Context, path string) (Fingerprint, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Fingerprint{}, errors.New("metadata: file path is required")
	}
	binary := strings.TrimSpace(f.Path)
	if binary == "" {
		binary = DefaultFPCalcPath
	}
	length := f.Length
	if length <= 0 {
		length = DefaultFingerprintLength
	}

	cmd := exec.CommandContext(ctx, binary, "-json", "-length", strconv.Itoa(length), path)
	stdout, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return Fingerprint{}, fmt.Errorf("metadata: fpcalc %s: %s", path, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return Fingerprint{}, fmt.Errorf("metadata: run fpcalc for %s: %w", path, err)
	}

	var payload struct {
		Duration    float64 `json:"duration"`
		Fingerprint string  `json:"fingerprint"`
	}
	if err := json.Unmarshal(stdout, &payload); err != nil {
		return Fingerprint{}, fmt.Errorf("metadata: decode fpcalc output for %s: %w", path, err)
	}
	if strings.TrimSpace(payload.Fingerprint) == "" {
		return Fingerprint{}, fmt.Errorf("metadata: fpcalc returned no fingerprint for %s", path)
	}
	duration := int(payload.Duration)
	if duration <= 0 {
		duration = length
	}
	return Fingerprint{Value: payload.Fingerprint, Duration: duration}, nil
}

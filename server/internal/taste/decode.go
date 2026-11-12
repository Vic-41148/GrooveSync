// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package taste

import (
	"encoding/json"
	"io"
)

// DecodeEvents reads a sync push payload. Validation stays in Append so the
// same rules apply to every caller, including the ones that do not come over
// HTTP.
func DecodeEvents(r io.Reader) ([]EventRecord, error) {
	var payload struct {
		Events []EventRecord `json:"events"`
	}
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Events, nil
}

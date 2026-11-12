// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package taste

import (
	"bytes"
	"testing"
)

func FuzzDecodeEvents(f *testing.F) {
	f.Add([]byte(`{"events":[]}`))
	f.Add([]byte(`{"events":[{"source":"local","id":"e1","device_id":"d","canonical_id":"t","type":"play","hlc":{"wall":"2026-01-01T00:00:00Z","counter":1,"node":"n"}}]}`))
	f.Add([]byte(`{"events":[{"hlc":{"wall":"0001-01-01T00:00:00Z"}}]}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{`))

	f.Fuzz(func(t *testing.T, raw []byte) {
		events, err := DecodeEvents(bytes.NewReader(raw))
		if err != nil {
			return
		}
		for _, event := range events {
			_ = event.Validate()
			if event.HLC.Node == "" {
				continue
			}
			parsed, err := ParseHLC(event.HLC.Format())
			if err != nil {
				t.Fatalf("clock %v does not round-trip: %v", event.HLC, err)
			}
			if !parsed.Wall.Equal(event.HLC.Wall) || parsed.Counter != event.HLC.Counter || parsed.Node != event.HLC.Node {
				t.Fatalf("clock round-trip = %v, want %v", parsed, event.HLC)
			}
		}
	})
}

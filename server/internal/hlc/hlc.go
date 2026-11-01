// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package hlc

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type Hlc struct {
	Wall    int64  `json:"-"`
	Counter int    `json:"-"`
	Node    string `json:"-"`
	Raw     string `json:"-"`
}

func Parse(s string) (Hlc, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return Hlc{}, fmt.Errorf("invalid hlc %q", s)
	}
	var wall int64
	var counter int
	if _, err := fmt.Sscanf(parts[0], "%d", &wall); err != nil {
		return Hlc{}, err
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &counter); err != nil {
		return Hlc{}, err
	}
	return Hlc{Wall: wall, Counter: counter, Node: parts[2], Raw: s}, nil
}

func (h Hlc) Compare(other Hlc) int {
	if h.Wall != other.Wall {
		if h.Wall < other.Wall {
			return -1
		}
		return 1
	}
	if h.Counter != other.Counter {
		if h.Counter < other.Counter {
			return -1
		}
		return 1
	}
	return strings.Compare(h.Node, other.Node)
}

type Vector struct {
	A   string `json:"a"`
	B   string `json:"b"`
	Max string `json:"max"`
}

func LoadVectors(path string) ([]Vector, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v []Vector
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return v, nil
}

func Max(a, b string) string {
	ha, _ := Parse(a)
	hb, _ := Parse(b)
	if ha.Compare(hb) >= 0 {
		return a
	}
	return b
}

var _ = sort.Strings

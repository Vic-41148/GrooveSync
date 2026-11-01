// Package ntfy implements the HTTP client for an ntfy server. The ntfy
// server is used under its Apache-2.0 license arm.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package ntfy

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
)

// Publisher sends a message to an ntfy topic.
type Publisher interface {
	Publish(ctx context.Context, topic, message string) error
}

// HTTPPublisher publishes to an ntfy server over HTTP.
type HTTPPublisher struct {
	BaseURL string
	Client  *http.Client
}

func (p HTTPPublisher) Publish(ctx context.Context, topic, message string) error {
	if topic == "" {
		return fmt.Errorf("ntfy: topic required")
	}
	base := p.BaseURL
	if base == "" {
		base = "https://ntfy.sh"
	}
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/"+topic, bytes.NewBufferString(message))
	if err != nil {
		return fmt.Errorf("ntfy: build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("ntfy: publish: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy: publish status %d", resp.StatusCode)
	}
	return nil
}

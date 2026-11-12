// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

// Command reachability exercises the syncd file routes over a slow or
// intermittent link. It uploads a generated file, stops part way, resumes from
// the offset the server reports, then downloads the file and compares hashes.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type uploadStatus struct {
	Hash     string `json:"hash"`
	Received int64  `json:"received"`
	Total    int64  `json:"total"`
	Complete bool   `json:"complete"`
	Path     string `json:"path"`
}

type probe struct {
	client   *http.Client
	base     string
	token    string
	deviceID string
}

func main() {
	server := flag.String("server", "http://127.0.0.1:8080", "syncd base URL")
	token := flag.String("token", os.Getenv("SYNCD_ADMIN_TOKEN"), "bearer token; device token also works")
	deviceID := flag.String("device-id", "", "device id sent as X-Device-Id; required with a device token")
	size := flag.Int64("size", 100<<20, "generated file size in bytes")
	chunk := flag.Int64("chunk", 4<<20, "upload chunk size in bytes")
	stopAfter := flag.Int64("stop-after", 32<<20, "bytes to upload before the simulated interruption")
	workdir := flag.String("workdir", "", "directory for the test files; defaults to the system temp dir")
	keep := flag.Bool("keep", false, "keep the generated and downloaded files")
	timeout := flag.Duration("timeout", 10*time.Minute, "overall timeout")
	flag.Parse()

	if *size <= 0 || *chunk <= 0 || *stopAfter <= 0 {
		fmt.Fprintln(os.Stderr, "size, chunk and stop-after must be positive")
		os.Exit(2)
	}
	if *stopAfter >= *size {
		fmt.Fprintln(os.Stderr, "stop-after must be smaller than size")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	p := &probe{
		client:   &http.Client{Timeout: 0},
		base:     strings.TrimRight(*server, "/"),
		token:    strings.TrimSpace(*token),
		deviceID: strings.TrimSpace(*deviceID),
	}
	if err := run(ctx, p, *size, *chunk, *stopAfter, *workdir, *keep); err != nil {
		fmt.Fprintf(os.Stderr, "reachability probe failed: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, p *probe, size, chunk, stopAfter int64, workdir string, keep bool) error {
	if err := p.probe(ctx); err != nil {
		return err
	}

	dir := workdir
	if dir == "" {
		var err error
		dir, err = os.MkdirTemp("", "reachability-*")
		if err != nil {
			return err
		}
		if !keep {
			defer os.RemoveAll(dir)
		}
	}
	name := "reachability.bin"
	path := filepath.Join(dir, name)
	sum, err := generate(path, size)
	if err != nil {
		return err
	}
	fmt.Printf("generated %s (%d bytes, sha256 %s)\n", path, size, sum)

	start := time.Now()
	if err := p.uploadRange(ctx, path, sum, name, 0, stopAfter, size, chunk); err != nil {
		return fmt.Errorf("first pass: %w", err)
	}
	fmt.Printf("first pass uploaded %d of %d bytes in %s\n", stopAfter, size, time.Since(start).Round(time.Millisecond))

	status, err := p.status(ctx, sum)
	if err != nil {
		return err
	}
	fmt.Printf("server holds %d of %d bytes (complete=%v)\n", status.Received, status.Total, status.Complete)
	if status.Complete {
		return fmt.Errorf("server completed the upload before the interruption; nothing to resume")
	}
	if status.Received != stopAfter {
		return fmt.Errorf("server received %d bytes, want %d", status.Received, stopAfter)
	}

	resume := time.Now()
	if err := p.uploadRange(ctx, path, sum, name, status.Received, size, size, chunk); err != nil {
		return fmt.Errorf("resume: %w", err)
	}
	fmt.Printf("resumed from %d and uploaded the rest in %s\n", status.Received, time.Since(resume).Round(time.Millisecond))

	status, err = p.status(ctx, sum)
	if err != nil {
		return err
	}
	if !status.Complete {
		return fmt.Errorf("upload incomplete after resume: %d of %d bytes", status.Received, status.Total)
	}

	download := time.Since(start)
	out := filepath.Join(dir, name+".download")
	if err := p.download(ctx, sum, out); err != nil {
		return err
	}
	got, err := fileHash(out)
	if err != nil {
		return err
	}
	if got != sum {
		return fmt.Errorf("hash mismatch: want %s got %s", sum, got)
	}
	fmt.Printf("downloaded and verified in %s total\n", download.Round(time.Millisecond))
	fmt.Printf("cleanup: remove the upload rows and files for %s on the server, then run a library sync\n", sum)
	return nil
}

// probe checks the unauthenticated health route and an authenticated route so a
// failure names the layer that broke.
func (p *probe) probe(ctx context.Context) error {
	start := time.Now()
	resp, err := p.get(ctx, "/healthz")
	if err != nil {
		return fmt.Errorf("healthz: %w", err)
	}
	defer resp.Body.Close()
	fmt.Printf("healthz: %s in %s\n", resp.Status, time.Since(start).Round(time.Millisecond))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned %s", resp.Status)
	}

	start = time.Now()
	resp, err = p.get(ctx, "/sync/v1/meta")
	if err != nil {
		return fmt.Errorf("meta: %w", err)
	}
	defer resp.Body.Close()
	fmt.Printf("meta: %s in %s\n", resp.Status, time.Since(start).Round(time.Millisecond))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("meta returned %s; check the token and X-Device-Id", resp.Status)
	}
	return nil
}

func (p *probe) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.base+path, nil)
	if err != nil {
		return nil, err
	}
	p.authorize(req)
	return p.client.Do(req)
}

func (p *probe) authorize(req *http.Request) {
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	if p.deviceID != "" {
		req.Header.Set("X-Device-Id", p.deviceID)
	}
}

// uploadRange sends [start, end) in chunks. The server only accepts a chunk
// that continues its part file, so the caller must pass the offset it reported.
func (p *probe) uploadRange(ctx context.Context, path, hash, name string, start, end, total, chunk int64) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	for cursor := start; cursor < end; {
		stop := cursor + chunk
		if stop > end {
			stop = end
		}
		if _, err := f.Seek(cursor, io.SeekStart); err != nil {
			return err
		}
		body := io.LimitReader(f, stop-cursor)
		status, err := p.putRange(ctx, hash, name, body, cursor, stop-1, total)
		if err != nil {
			return err
		}
		fmt.Printf("uploaded bytes %d-%d (%d of %d)\n", cursor, stop-1, status.Received, total)
		cursor = stop
	}
	return nil
}

func (p *probe) putRange(ctx context.Context, hash, name string, body io.Reader, start, end, total int64) (uploadStatus, error) {
	query := url.Values{"name": {name}}
	target := fmt.Sprintf("%s/sync/v1/library/uploads/%s?%s", p.base, hash, query.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, body)
	if err != nil {
		return uploadStatus{}, err
	}
	req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
	p.authorize(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return uploadStatus{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusCreated {
		return uploadStatus{}, fmt.Errorf("upload failed: %s - %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var status uploadStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return uploadStatus{}, fmt.Errorf("decode upload status: %w", err)
	}
	return status, nil
}

func (p *probe) status(ctx context.Context, hash string) (uploadStatus, error) {
	resp, err := p.get(ctx, "/sync/v1/library/uploads/"+hash)
	if err != nil {
		return uploadStatus{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return uploadStatus{}, fmt.Errorf("upload status: %s - %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var status uploadStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return uploadStatus{}, fmt.Errorf("decode upload status: %w", err)
	}
	return status, nil
}

func (p *probe) download(ctx context.Context, hash, dst string) error {
	resp, err := p.get(ctx, "/sync/v1/library/files/"+hash)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download: %s - %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// generate writes a file of the requested size and returns its SHA-256.
func generate(path string, size int64) (string, error) {
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hash := sha256.New()
	block := make([]byte, 1<<20)
	if _, err := rand.Read(block); err != nil {
		return "", err
	}
	var written int64
	for written < size {
		n := int64(len(block))
		if remaining := size - written; remaining < n {
			n = remaining
		}
		part := block[:n]
		if _, err := f.Write(part); err != nil {
			return "", err
		}
		if _, err := hash.Write(part); err != nil {
			return "", err
		}
		written += n
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

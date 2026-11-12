// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

//go:build cgo

package library

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestScannerReadsTagLibMetadata(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not available")
	}
	root := t.TempDir()
	audio := filepath.Join(root, "song.wav")
	cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-metadata", "title=Song Title",
		"-metadata", "artist=Artist One; Artist Two",
		"-metadata", "album=Album Name",
		audio)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v\n%s", err, out)
	}

	db := openScannerDB(t)
	scanner := Scanner{DB: db, Root: root, StabilityInterval: time.Millisecond}
	count, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("scanned = %d, want 1", count)
	}

	var title, artist, album string
	var duration int
	if err := db.QueryRowContext(context.Background(), `SELECT title, artist, album, duration_ms FROM track`).Scan(&title, &artist, &album, &duration); err != nil {
		t.Fatalf("query track: %v", err)
	}
	if title != "Song Title" {
		t.Fatalf("title = %q, want Song Title", title)
	}
	if artist != "Artist One, Artist Two" {
		t.Fatalf("artist = %q, want Artist One, Artist Two", artist)
	}
	if album != "Album Name" {
		t.Fatalf("album = %q, want Album Name", album)
	}
	if duration <= 0 {
		t.Fatalf("duration = %d, want > 0", duration)
	}
}

func FuzzReadTagLib(f *testing.F) {
	f.Add([]byte("RIFF....WAVEfmt "))
	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "fuzz.bin")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		_, _ = readTagLib(path)
	})
}

func TestScannerReadsSyntheticSineWav(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	audio := filepath.Join(root, "sine.wav")
	if err := writeSyntheticSineWav(audio, 44100, 1); err != nil {
		t.Fatalf("write wav: %v", err)
	}

	db := openScannerDB(t)
	scanner := Scanner{DB: db, Root: root, StabilityInterval: time.Millisecond}
	count, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("scanned = %d, want 1", count)
	}
	var duration int
	if err := db.QueryRowContext(context.Background(), `SELECT duration_ms FROM track`).Scan(&duration); err != nil {
		t.Fatalf("query track: %v", err)
	}
	if duration < 900 || duration > 1100 {
		t.Fatalf("duration = %d ms, want about 1000", duration)
	}
}

func writeSyntheticSineWav(path string, sampleRate, seconds int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	numSamples := sampleRate * seconds
	dataSize := numSamples * 2
	riffSize := 36 + dataSize
	if err := binary.Write(f, binary.LittleEndian, []byte("RIFF")); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(riffSize)); err != nil {
		return err
	}
	if _, err := f.Write([]byte("WAVEfmt ")); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(16)); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint16(1)); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint16(1)); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(sampleRate)); err != nil {
		return err
	}
	byteRate := uint32(sampleRate * 2)
	if err := binary.Write(f, binary.LittleEndian, byteRate); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint16(2)); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint16(16)); err != nil {
		return err
	}
	if _, err := f.Write([]byte("data")); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(dataSize)); err != nil {
		return err
	}
	for i := 0; i < numSamples; i++ {
		value := int16(32767 * math.Sin(2*math.Pi*440*float64(i)/float64(sampleRate)))
		if err := binary.Write(f, binary.LittleEndian, value); err != nil {
			return err
		}
	}
	return nil
}

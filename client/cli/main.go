// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	server := flag.String("server", "http://127.0.0.1:8080", "syncd server base URL")
	account := flag.String("account", "acct-1", "account id to connect")
	open := flag.Bool("open", false, "try to open the auth URL in a browser")
	mode := flag.String("mode", "auth", "operation mode: auth or upload")
	file := flag.String("file", "", "file to upload when mode=upload")
	chunkSize := flag.Int64("chunk-size", 4*1024*1024, "upload chunk size in bytes")
	flag.Parse()

	switch *mode {
	case "auth":
		startURL := fmt.Sprintf("%s/sync/v1/connectors/spotify/%s/start-auth?return_to=/", strings.TrimRight(*server, "/"), *account)
		resp, err := http.Post(startURL, "application/json", nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "start-auth request failed: %v\n", err)
			os.Exit(2)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body := new(bytes.Buffer)
			_, _ = body.ReadFrom(resp.Body)
			fmt.Fprintf(os.Stderr, "start-auth failed: %s - %s\n", resp.Status, body.String())
			os.Exit(2)
		}
		var out struct {
			URL           string `json:"url"`
			State         string `json:"state"`
			CodeChallenge string `json:"code_challenge"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			fmt.Fprintf(os.Stderr, "decode response: %v\n", err)
			os.Exit(2)
		}
		fmt.Printf("Open this URL in your browser to authorize:\n%s\n\n", out.URL)
		fmt.Printf("state: %s\ncode_challenge: %s\n\n", out.State, out.CodeChallenge)

		if *open {
			// best-effort: try xdg-open, fallback to printing the URL
			_ = exec.Command("xdg-open", out.URL).Start()
		}

		fmt.Print("If the server is reachable it will handle the callback.\n")
		fmt.Print("Otherwise paste the 'code' query parameter from the redirect URL here (empty to exit): ")
		r := bufio.NewReader(os.Stdin)
		line, _ := r.ReadString('\n')
		code := strings.TrimSpace(line)
		if code == "" {
			fmt.Println("No code provided, exiting.")
			return
		}

		// POST complete-auth
		postURL := fmt.Sprintf("%s/sync/v1/connectors/spotify/%s/complete-auth", strings.TrimRight(*server, "/"), *account)
		payload := map[string]string{"code": code, "state": out.State}
		buf := new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(payload); err != nil {
			fmt.Fprintf(os.Stderr, "encode request: %v\n", err)
			os.Exit(2)
		}
		req, _ := http.NewRequest(http.MethodPost, postURL, buf)
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{}
		resp2, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "complete-auth request failed: %v\n", err)
			os.Exit(2)
		}
		defer resp2.Body.Close()
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(resp2.Body)
		fmt.Printf("complete-auth result: %s\n%s\n", resp2.Status, strings.TrimSpace(body.String()))

	case "upload":
		if *file == "" {
			fmt.Fprintln(os.Stderr, "-file is required in upload mode")
			os.Exit(2)
		}
		if err := uploadFile(*server, *file, *chunkSize); err != nil {
			fmt.Fprintf(os.Stderr, "upload failed: %v\n", err)
			os.Exit(2)
		}

	default:
		fmt.Fprintf(os.Stderr, "unknown mode: %s\n", *mode)
		os.Exit(2)
	}
}

// uploadFile uploads a file in chunks and then downloads it back.
func uploadFile(server, filename string, chunkSize int64) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	size := info.Size()
	hash := sha256.New()
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := io.Copy(hash, f); err != nil {
		return err
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	// rewind
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}

	// chunked upload
	client := &http.Client{}
	var start int64
	for start < size {
		end := start + chunkSize - 1
		if end >= size {
			end = size - 1
		}
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			return err
		}
		part := io.LimitReader(f, end-start+1)
		putURL := fmt.Sprintf("%s/sync/v1/library/uploads/%s?name=%s", strings.TrimRight(server, "/"), sum, filepath.Base(filename))
		req, err := http.NewRequest(http.MethodPut, putURL, part)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusCreated {
			return fmt.Errorf("upload failed: %s - %s", resp.Status, strings.TrimSpace(body.String()))
		}
		fmt.Printf("uploaded bytes %d-%d (%s)\n", start, end, resp.Status)
		start = end + 1
	}

	// poll status
	statusURL := fmt.Sprintf("%s/sync/v1/library/uploads/%s", strings.TrimRight(server, "/"), sum)
	for i := 0; i < 20; i++ {
		resp2, err := http.Get(statusURL)
		if err != nil {
			return err
		}
		var st struct {
			Hash     string `json:"hash"`
			Received int64  `json:"received"`
			Total    int64  `json:"total"`
			Complete bool   `json:"complete"`
			Path     string `json:"path"`
		}
		_ = json.NewDecoder(resp2.Body).Decode(&st)
		resp2.Body.Close()
		fmt.Printf("status: %d/%d complete=%v\n", st.Received, st.Total, st.Complete)
		if st.Complete {
			// download the file
			fileURL := fmt.Sprintf("%s/sync/v1/library/files/%s", strings.TrimRight(server, "/"), sum)
			r, err := http.Get(fileURL)
			if err != nil {
				return err
			}
			defer r.Body.Close()
			out := filepath.Base(filename) + ".download"
			o, err := os.Create(out)
			if err != nil {
				return err
			}
			if _, err := io.Copy(o, r.Body); err != nil {
				o.Close()
				return err
			}
			o.Close()
			fmt.Printf("downloaded to %s\n", out)
			// verify downloaded file matches original hash
			df, err := os.Open(out)
			if err != nil {
				return err
			}
			defer df.Close()
			dh := sha256.New()
			if _, err := io.Copy(dh, df); err != nil {
				return err
			}
			dsum := hex.EncodeToString(dh.Sum(nil))
			if dsum != sum {
				return fmt.Errorf("hash mismatch: want %s got %s", sum, dsum)
			}
			fmt.Printf("verified: hash %s\n", sum)
			return nil
		}
		// simple sleep
		_ = exec.Command("sleep", "1").Run()
	}
	return fmt.Errorf("upload did not complete in time")
}

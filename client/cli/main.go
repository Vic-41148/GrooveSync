// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strings"
)

func main() {
	server := flag.String("server", "http://127.0.0.1:8080", "syncd server base URL")
	account := flag.String("account", "acct-1", "account id to connect")
	open := flag.Bool("open", false, "try to open the auth URL in a browser")
	flag.Parse()

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
}

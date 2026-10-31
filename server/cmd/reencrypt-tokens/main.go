// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package main

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/Vic-41148/groovesync/server/internal/connectors"
	"github.com/Vic-41148/groovesync/server/internal/store"
	_ "modernc.org/sqlite"
)

func main() {
	var dbPath string
	var keyHex string
	var dryRun bool
	var yes bool
	var online bool
	var kid string
	var rotate bool
	var keyArgs []string
	flag.StringVar(&dbPath, "db", "syncd.db", "path to SQLite database")
	flag.StringVar(&keyHex, "key-hex", os.Getenv("SYNCD_CONNECTOR_KEY_HEX"), "hex-encoded AES key (or set SYNCD_CONNECTOR_KEY_HEX)")
	flag.Func("key", "add a key as kid=HEX, may be repeated for multiple keys", func(s string) error { keyArgs = append(keyArgs, s); return nil })
	flag.BoolVar(&dryRun, "dry-run", true, "scan and report what would be changed without writing")
	flag.BoolVar(&yes, "yes", false, "apply changes without interactive prompt")
	flag.BoolVar(&online, "online", false, "run in online mode: per-row transactional updates without taking a global transaction (no full downtime)")
	flag.StringVar(&kid, "kid", "", "key id to record on encrypted rows (optional)")
	var rotateOnly bool
	flag.BoolVar(&rotate, "rotate", false, "also re-encrypt rows that already appear encrypted (rotate to --kid)")
	flag.BoolVar(&rotateOnly, "rotate-only", false, "only rotate rows encrypted with --from-kid to the destination key (requires --from-kid)")
	var fromKid string
	var toKid string
	flag.StringVar(&fromKid, "from-kid", "", "source key id to rotate from (required for --rotate-only)")
	flag.StringVar(&toKid, "to-kid", "", "destination key id to rotate to (defaults to --kid primary)")
	flag.Parse()

	if strings.TrimSpace(keyHex) == "" && len(keyArgs) == 0 {
		log.Fatal("a hex key must be supplied via --key-hex, --key=KID=HEX, or SYNCD_CONNECTOR_KEY_HEX")
	}

	// Prepare key material. Support either a single legacy key via keyHex or
	// multiple keys passed with repeated --key flags as kid=HEX.
	var encryptionKey []byte
	var keysMap map[string][]byte
	if len(keyArgs) > 0 {
		keysMap = make(map[string][]byte)
		for _, ka := range keyArgs {
			parts := strings.SplitN(ka, "=", 2)
			if len(parts) != 2 {
				log.Fatalf("invalid --key value %q, want kid=HEX", ka)
			}
			kidPart := strings.TrimSpace(parts[0])
			hexPart := strings.TrimSpace(parts[1])
			if kidPart == "" || hexPart == "" {
				log.Fatalf("invalid --key value %q, want kid=HEX", ka)
			}
			kb, err := hex.DecodeString(hexPart)
			if err != nil {
				log.Fatalf("invalid hex for key %q: %v", kidPart, err)
			}
			if len(kb) != 16 && len(kb) != 24 && len(kb) != 32 {
				log.Fatalf("key %q must decode to 16, 24 or 32 bytes, got %d", kidPart, len(kb))
			}
			keysMap[kidPart] = kb
		}
		// Ensure primary kid is set when multiple keys provided.
		if strings.TrimSpace(kid) == "" {
			log.Fatal("when supplying multiple --key entries you must set --kid to the primary key id")
		}
		p := strings.TrimSpace(kid)
		pk, ok := keysMap[p]
		if !ok {
			log.Fatalf("primary key id %q not found among --key values", p)
		}
		encryptionKey = pk
	} else {
		// Single-key legacy path using keyHex
		key, err := hex.DecodeString(strings.TrimSpace(keyHex))
		if err != nil {
			log.Fatalf("invalid key hex: %v", err)
		}
		if len(key) != 16 && len(key) != 24 && len(key) != 32 {
			log.Fatalf("key must decode to 16, 24 or 32 bytes, got %d", len(key))
		}
		encryptionKey = key
	}

	if dryRun {
		log.Println("running in dry-run mode: no changes will be written")
	}
	if online && dryRun {
		log.Println("running in online dry-run mode")
	}

	db, err := sql.Open("sqlite", store.DSNForTest(dbPath))
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// Ensure schema is migrated so tables exist
	if err := store.Migrate(context.Background(), db); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	repo := connectors.NewRepo(db)
	if keysMap != nil {
		repo.SetKeys(keysMap, strings.TrimSpace(kid))
	} else {
		repo.SetKey(encryptionKey)
	}

	if rotateOnly && strings.TrimSpace(fromKid) == "" {
		log.Fatal("--rotate-only requires --from-kid to be set")
	}

	// run the re-encryption/rotation operation
	if err := runReencrypt(context.Background(), db, encryptionKey, keysMap, strings.TrimSpace(kid), dryRun, online, rotate, rotateOnly, yes, strings.TrimSpace(fromKid), strings.TrimSpace(toKid)); err != nil {
		log.Fatalf("reencrypt: %v", err)
	}
	return

}

// runReencrypt performs the scanning and update operations. It is factored out
// so tests can call it directly.
func runReencrypt(ctx context.Context, db *sql.DB, encryptionKey []byte, keysMap map[string][]byte, primaryKid string, dryRun, online, rotate, rotateOnly bool, yes bool, fromKid, toKid string) error {
	// Ensure destination kid when rotating-only
	if rotateOnly {
		if keysMap == nil {
			return fmt.Errorf("rotate-only requires multiple keys provided via --key entries")
		}
		if fromKid == "" {
			return fmt.Errorf("rotate-only requires --from-kid")
		}
		if toKid == "" {
			toKid = primaryKid
		}
		if toKid == "" {
			return fmt.Errorf("rotate-only requires a destination key id via --to-kid or --kid primary")
		}
		if _, ok := keysMap[fromKid]; !ok {
			return fmt.Errorf("from-kid %q not found among provided keys", fromKid)
		}
		if _, ok := keysMap[toKid]; !ok {
			return fmt.Errorf("to-kid %q not found among provided keys", toKid)
		}
	}

	// Build query selecting key_id as well
	rows, err := db.QueryContext(ctx, `SELECT provider, account_id, access_token, refresh_token, key_id FROM connector_token`)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Println("no connector_token rows")
			return nil
		}
		return fmt.Errorf("query connector_token: %w", err)
	}
	defer rows.Close()

	type row struct {
		provider, account string
		a, r              string
	}
	var toUpdate []row
	var total int
	for rows.Next() {
		var p, aID, at, rt, kidCol sql.NullString
		if err := rows.Scan(&p, &aID, &at, &rt, &kidCol); err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		total++
		atStr := ""
		rtStr := ""
		if at.Valid {
			atStr = at.String
		}
		if rt.Valid {
			rtStr = rt.String
		}
		kidStr := ""
		if kidCol.Valid {
			kidStr = kidCol.String
		}
		need := false
		// rotate-only: target rows with matching key_id
		if rotateOnly {
			// only consider rows whose key_id equals fromKid
			if kidStr != fromKid {
				continue
			}
			// only attempt rows that are non-empty
			if atStr != "" || rtStr != "" {
				need = true
			}
		} else {
			// if not hex => consider plaintext and encrypt
			if _, err := hex.DecodeString(atStr); err != nil && atStr != "" {
				need = true
			}
			if _, err := hex.DecodeString(rtStr); err != nil && rtStr != "" {
				need = true
			}
			// rotate flag forces re-encryption even when values are already hex
			if rotate && (atStr != "" || rtStr != "") {
				need = true
			}
		}
		if need {
			toUpdate = append(toUpdate, row{provider: p.String, account: aID.String, a: atStr, r: rtStr})
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate rows: %w", err)
	}

	log.Printf("found %d connector_token rows, %d appear to need encryption", total, len(toUpdate))
	if len(toUpdate) == 0 {
		return nil
	}
	if dryRun {
		for _, t := range toUpdate {
			log.Printf("would encrypt provider=%s account=%s (access_len=%d refresh_len=%d)", t.provider, t.account, len(t.a), len(t.r))
		}
		return nil
	}
	if !yes {
		var resp string
		fmt.Printf("apply encryption to %d rows in %s? Type 'yes' to proceed: ", len(toUpdate), "db")
		if _, err := fmt.Scanln(&resp); err != nil || strings.ToLower(strings.TrimSpace(resp)) != "yes" {
			return fmt.Errorf("aborted")
		}
	}

	var applied int
	// prepare the key_id value to write (nil when empty)
	var keyID any
	if strings.TrimSpace(primaryKid) != "" {
		keyID = strings.TrimSpace(primaryKid)
	} else {
		keyID = nil
	}
	// For rotate-only operations the destination key id should be used
	if rotateOnly {
		if toKid == "" {
			toKid = primaryKid
		}
		keyID = toKid
	}
	if online {
		// Online mode: perform per-row optimistic updates so the server can stay
		// running. Each row update is applied only if the row still matches the
		// observed values to avoid clobbering concurrent changes.
		updStmt, err := db.PrepareContext(ctx, `UPDATE connector_token SET access_token = ?, refresh_token = ?, key_id = ? WHERE provider = ? AND account_id = ? AND access_token = ? AND refresh_token = ?`)
		if err != nil {
			return fmt.Errorf("prepare update: %w", err)
		}
		defer updStmt.Close()

		for _, t := range toUpdate {
			at := t.a
			rt := t.r
			var newAt, newRt any = at, rt
			// Determine encryption path: rotate-only uses keysMap[from]->keysMap[to]
			if rotateOnly {
				// decrypt with from key
				fromKey := keysMap[fromKid]
				// decrypt access token
				if at != "" {
					pt, ok, err := connectors.TryDecryptToken(fromKey, at)
					if err != nil {
						return fmt.Errorf("decrypt access token for %s/%s: %w", t.provider, t.account, err)
					}
					if !ok {
						log.Printf("skipping %s/%s: access token did not decrypt with from-kid", t.provider, t.account)
					} else {
						enc, err := connectors.EncryptToken(keysMap[toKid], pt)
						if err != nil {
							return fmt.Errorf("encrypt access token for %s/%s: %w", t.provider, t.account, err)
						}
						newAt = enc
					}
				}
				if rt != "" {
					pt, ok, err := connectors.TryDecryptToken(fromKey, rt)
					if err != nil {
						return fmt.Errorf("decrypt refresh token for %s/%s: %w", t.provider, t.account, err)
					}
					if !ok {
						log.Printf("skipping %s/%s: refresh token did not decrypt with from-kid", t.provider, t.account)
					} else {
						enc, err := connectors.EncryptToken(keysMap[toKid], pt)
						if err != nil {
							return fmt.Errorf("encrypt refresh token for %s/%s: %w", t.provider, t.account, err)
						}
						newRt = enc
					}
				}
			} else {
				if at != "" {
					enc, err := connectors.EncryptToken(encryptionKey, at)
					if err != nil {
						return fmt.Errorf("encrypt access token for %s/%s: %w", t.provider, t.account, err)
					}
					newAt = enc
				}
				if rt != "" {
					enc, err := connectors.EncryptToken(encryptionKey, rt)
					if err != nil {
						return fmt.Errorf("encrypt refresh token for %s/%s: %w", t.provider, t.account, err)
					}
					newRt = enc
				}
			}
			if dryRun {
				log.Printf("would update provider=%s account=%s (set key_id=%v)", t.provider, t.account, keyID)
				continue
			}
			res, err := updStmt.ExecContext(ctx, newAt, newRt, keyID, t.provider, t.account, t.a, t.r)
			if err != nil {
				return fmt.Errorf("update %s/%s: %w", t.provider, t.account, err)
			}
			n, _ := res.RowsAffected()
			if n == 1 {
				applied++
			} else {
				log.Printf("skipped %s/%s: concurrent change detected", t.provider, t.account)
			}
		}
		log.Printf("applied encryption to %d rows (online)", applied)
		return nil
	}

	// Offline mode: apply all updates inside a single transaction. This is
	// simpler but requires the server to be stopped or the DB not to be
	// concurrently mutated.
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	updStmt, err := tx.PrepareContext(ctx, `UPDATE connector_token SET access_token = ?, refresh_token = ?, key_id = ? WHERE provider = ? AND account_id = ?`)
	if err != nil {
		return fmt.Errorf("prepare update: %w", err)
	}
	defer updStmt.Close()

	for _, t := range toUpdate {
		at := t.a
		rt := t.r
		if rotateOnly {
			// decrypt with from key and re-encrypt with to key
			fromKey := keysMap[fromKid]
			if at != "" {
				pt, ok, err := connectors.TryDecryptToken(fromKey, at)
				if err != nil {
					return fmt.Errorf("decrypt access token for %s/%s: %w", t.provider, t.account, err)
				}
				if ok {
					enc, err := connectors.EncryptToken(keysMap[toKid], pt)
					if err != nil {
						return fmt.Errorf("encrypt access token for %s/%s: %w", t.provider, t.account, err)
					}
					at = enc
				} else {
					log.Printf("skipping %s/%s access token: didn't decrypt with from-kid", t.provider, t.account)
				}
			}
			if rt != "" {
				pt, ok, err := connectors.TryDecryptToken(fromKey, rt)
				if err != nil {
					return fmt.Errorf("decrypt refresh token for %s/%s: %w", t.provider, t.account, err)
				}
				if ok {
					enc, err := connectors.EncryptToken(keysMap[toKid], pt)
					if err != nil {
						return fmt.Errorf("encrypt refresh token for %s/%s: %w", t.provider, t.account, err)
					}
					rt = enc
				} else {
					log.Printf("skipping %s/%s refresh token: didn't decrypt with from-kid", t.provider, t.account)
				}
			}
		} else {
			if at != "" {
				enc, err := connectors.EncryptToken(encryptionKey, at)
				if err != nil {
					return fmt.Errorf("encrypt access token for %s/%s: %w", t.provider, t.account, err)
				}
				at = enc
			}
			if rt != "" {
				enc, err := connectors.EncryptToken(encryptionKey, rt)
				if err != nil {
					return fmt.Errorf("encrypt refresh token for %s/%s: %w", t.provider, t.account, err)
				}
				rt = enc
			}
		}
		if dryRun {
			log.Printf("would update provider=%s account=%s (set key_id=%v)", t.provider, t.account, keyID)
			continue
		}
		if _, err := updStmt.ExecContext(ctx, at, rt, keyID, t.provider, t.account); err != nil {
			return fmt.Errorf("update %s/%s: %w", t.provider, t.account, err)
		}
		applied++
	}
	if dryRun {
		return nil
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	log.Printf("applied encryption to %d rows (offline)", applied)
	return nil
}

# reencrypt-tokens

Offline and online re-encryption tool for connector tokens.

Usage

- Dry run (default): scans the database and reports rows that would be encrypted.
  go run ./server/cmd/reencrypt-tokens --db /path/to/syncd.db

- Apply changes (offline transactional mode): stop the server, then run:
  go run ./server/cmd/reencrypt-tokens --db /path/to/syncd.db --dry-run=false --yes --key-hex <HEX>

- Online (no-downtime) mode: run while the server is running. Each row is
  updated only if it still matches the snapshot values to avoid clobbering
  concurrent updates:
  go run ./server/cmd/reencrypt-tokens --db /path/to/syncd.db --dry-run=false --online --yes --key-hex <HEX>

Key material

Supply the AES key as hex via --key-hex or the SYNCD_CONNECTOR_KEY_HEX
environment variable. The key must decode to 16, 24 or 32 bytes (AES-128/192/256).

Rotation (multi-key)

When you manage multiple keys (provide repeated --key kid=HEX and set --kid to the primary), use --rotate-only to move rows encrypted with a specific KID to the primary key without affecting other rows:

- Rotate rows encrypted with KID "old" to the primary "new" (offline):
  go run ./server/cmd/reencrypt-tokens --db /path/to/syncd.db --dry-run=false --yes --online=false --key old=<HEX> --key new=<HEX> --kid new --rotate-only --from-kid old

The rotate-only mode only targets rows whose connector_token.key_id matches --from-kid and writes the destination key_id on success. It decrypts with the source key and re-encrypts with the destination key, skipping rows that fail to decrypt with the source.

Safety

- Always back up your database before running the tool.
- Prefer the offline transactional mode when possible. The online mode is
  offered to avoid downtime but may skip rows that changed concurrently — you
  can re-run it until all rows are encrypted.

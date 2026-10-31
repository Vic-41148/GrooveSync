Runbook — reencrypt-tokens

Purpose

- Provide safe, repeatable instructions for migrating connector tokens at rest to AES-GCM encrypted blobs, and for rotating encryption keys.

Key concepts

- Storage: encrypted tokens are stored as hex(nonce || ciphertext). Existing plaintext rows remain readable and untouched unless explicitly re-encrypted.
- Key provisioning modes:
  - Legacy single-key: supply a single key via --key-hex or SYNCD_CONNECTOR_KEY_HEX. This encrypts rows but does not record a key_id on rows.
  - Multi-key rotation: supply repeated --key kid=HEX and set --kid to the primary key id. New rows are encrypted under the primary and key_id is written.
- connector_token.key_id column records which KID a row was encrypted with (NULL for legacy single-key or plaintext rows).

Generating keys

1. Choose a random AES key of 16, 24, or 32 bytes (AES-128/192/256). For strong security prefer 32 bytes.
2. Encode the key as hex. Example using openssl:

   - Generate 32 random bytes and print hex:
     printf "$(xxd -p -l 32 /dev/urandom)\n"

   - Or using openssl (Linux/macOS):
     openssl rand -hex 32

3. Record the key id (KID). Use a short stable identifier (for example, a short timestamp or label like "2026-10-31" or "primary").

Provisioning keys to the server

- Single-key (legacy): set SYNCD_CONNECTOR_KEY_HEX to the hex-encoded key and restart syncd. The repo will use that key for encryption and decryption but will not write key_id on rows.
- Multi-key (recommended for rotation): start the re-encrypt tool with repeated --key flags and --kid set to the primary KID, or supply keys to the server similarly when it starts (cmd/syncd/main.go supports wiring multi-key configuration).

Safety and backups

- Always back up the database before running any migration or rotation tool.
- Prefer offline transactional mode when possible: stop the server, run the tool in offline mode (--online=false). Offline mode performs all updates inside a single transaction ensuring consistency.
- Online mode (--online) allows no-downtime rotations; each row update is applied only if the row still matches the observed snapshot to avoid clobbering concurrent writes. Re-run until no rows remain to be migrated.

Common operations

1) Dry-run (discover what would change)

   go run ./cmd/reencrypt-tokens --db /path/to/syncd.db

2) Apply encryption (offline, single-key)

   go run ./cmd/reencrypt-tokens --db /path/to/syncd.db --dry-run=false --yes --key-hex <HEX>

3) Apply encryption (online, single-key)

   go run ./cmd/reencrypt-tokens --db /path/to/syncd.db --dry-run=false --online --yes --key-hex <HEX>

4) Multi-key setup: add two keys and set primary

   go run ./cmd/reencrypt-tokens --db /path/to/syncd.db --dry-run=false --yes \
     --key old=<OLD_HEX> --key new=<NEW_HEX> --kid new

   This will configure the tool to encrypt with the primary key "new" and, when applied to plaintext rows, will write key_id = "new" on updated rows.

5) Rotate-only: re-encrypt rows that were encrypted with a specific KID

   - Offline (recommended when you can stop the server):
     go run ./cmd/reencrypt-tokens --db /path/to/syncd.db --dry-run=false --yes --key old=<OLD_HEX> --key new=<NEW_HEX> --kid new --rotate-only --from-kid old

   - Online (no downtime):
     go run ./cmd/reencrypt-tokens --db /path/to/syncd.db --dry-run=false --online --yes --key old=<OLD_HEX> --key new=<NEW_HEX> --kid new --rotate-only --from-kid old

   Notes:
   - rotate-only targets rows where connector_token.key_id = <from-kid>. It decrypts using the from key and re-encrypts with the destination key, then writes the destination key_id.
   - rotate-only will skip and log any rows that cannot be decrypted with the from key.

Troubleshooting

- If rows are not being updated in online mode, re-run the tool until no rows remain; concurrent updates may cause individual rows to be skipped.
- If decryption fails for some rows during rotation, ensure the correct source key is supplied. Unknown key_id values will trigger try-all-keys fallback only when reading, but rotate-only requires the from key to be supplied explicitly.

Audit and rollback

- Keep an exported backup copy of the connector_token table before running the tool. If you must roll back, restore from backup and stop using the new key.
- Rotation is hermetic: it rewrites token blobs; keep old keys accessible until you are confident no rows require them.

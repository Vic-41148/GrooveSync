# Threat model

Scope: the `syncd` companion, the clients that talk to it, the connector
imports, and the `provisioner`. The client app is still being built; this
document covers the server side that exists today.

## Assets

- The local music library and the files uploaded to `syncd`.
- Event history: plays, skips, likes and playlist changes.
- Device tokens and the admin token.
- Connector tokens for Spotify, YouTube Music and ListenBrainz.
- The optional yt-dlp cookie file.

## Trust boundaries

- Client to `syncd`: LAN, tailnet, or a public opt-in such as Funnel.
- `syncd` to external services: Spotify, YouTube Music, MusicBrainz,
  AcoustID, Cover Art Archive, Navidrome and ntfy.
- `provisioner` to `syncd` and to yt-dlp.
- `syncd` to the local filesystem and the SQLite database.

## Threats and mitigations

| Threat | Mitigation | Residual risk |
|---|---|---|
| Unauthenticated access from the LAN or tailnet | `SYNCD_ADMIN_TOKEN` is required when the listener is not loopback. `SYNCD_ALLOW_CIDRS` defaults to Tailscale CGNAT plus loopback. The admin token is compared in constant time. Device registration is rate limited. | A token is a bearer credential. Anyone who reads it can use it until it is rotated. |
| Accidental public exposure | `config.CheckExposure` refuses a non-loopback bind without an admin token. The README and compose file publish only to loopback. Funnel and port forwarding are opt-in. | A deliberate public bind is allowed once a token is set. `syncd` does not terminate TLS. |
| Stolen device token | Tokens are 32 random bytes, issued per device, and only a SHA-256 hash is stored. `DELETE /sync/v1/devices/{id}` revokes the device. Tokens are not logged. | There is no remote wipe. A stolen device keeps its local copy of the library. |
| Stolen admin token | The token is read from the environment, a file, or a flag. It is never stored in the database and never written to the repository. | One token grants full API access. There is no per-admin identity or audit trail. |
| Connector token theft at rest | Connector tokens are AES-GCM encrypted with `SYNCD_CONNECTOR_KEY_HEX`; the key is not stored in the database. | The key sits in the process environment. A host compromise exposes both key and ciphertext. |
| Malicious media file | TagLib parses the file; the scanner does not run as root; `FuzzReadTagLib` exercises the parser. | TagLib is a native dependency. A parser bug can still crash the process. |
| Malicious remote metadata | Every Spotify, ytmusicapi, MusicBrainz, AcoustID and Navidrome response is treated as untrusted. Bodies are size limited and validated. | A hostile response can still produce bad matches; the review queue is the backstop. |
| Supply chain | Actions are pinned by commit SHA, Dependabot has a 14-day cooldown, `go.sum` is committed, licences are checked, and gitleaks scans history and staged changes. | Dependencies can still be compromised upstream. |
| Provisioner abuse | yt-dlp runs as an external executable, cookies are off by default, datacenter egress is refused, and the daily cap limits downloads. | The provisioner is a separate opt-in service and is not part of the core app. |
| Replay of event pushes | Event IDs and hybrid logical clocks make pushes idempotent and order independent. | A captured request can be replayed; it does not change the result. |

## Out of scope

- Operating system or hypervisor compromise.
- Physical access to the server or a device.
- Tailscale, Navidrome or Spotify account compromise.
- Account bans or terms enforcement by Spotify or YouTube.
- Backups. They are tracked separately in `docs/todo/cross-cutting.md`.

## Open work

- TLS is expected to come from the tailnet or a reverse proxy. `syncd` has no
  built-in TLS listener.
- There is no per-admin identity and no audit log for administrative calls.
- The SQLite database is not encrypted at rest. File permissions and full-disk
  encryption are the current control.
- Device revocation is manual. A lost device is removed with the delete route.

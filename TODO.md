# GrooveSync TODO

Last updated: 2026-10-31. Branch `dev`, merged into `main` as `529d1c2`.
This file replaces the stale decision list that used to sit here.

Legend:

- `[OWNER]` needs your decision, account, credentials or a physical device.
- `[BLOCK]` blocks other work until it is done.
- `[CRIT]` security or data-loss risk. Do before exposing the service.
- `[LATER]` after the current phase, do not start early.
- Plain `[ ]` is ordinary work.

Sources, in order of authority: `docs/roadmap/roadmap-v1.1.md` (phases, decisions
D1 to D10), `docs/handbook/music-app-handbook.md` (chunk decisions, Day-1 list,
open items), `docs/handbook/music-app-handbook-audit.md`, `docs/research/`.
`docs/` is git-ignored and local only. Everything below is derived from those
files plus a read of the code on `dev`.

## 1. Where the project stands

| Phase | State |
|---|---|
| 0. Spikes and gate G0 | Decisions recorded; empirical spikes never run (ytmusicapi auth, Spotify live, Navidrome plugin validate). |
| 1. Unified read-only library | Server side done. No web UI (1.5). Local ingest is the Navidrome mirror, not a TagLib scanner (1.2). |
| 2. Write-back you trust | Server side done, including shadow/live, guards, tombstones, undo. No weekly contract job (2.5). |
| 3. Taste engine v1 | Done: HLC event log, explainable score, four mix kinds, backtest. |
| 4. Cross-device sync and client | Server side mostly done (4.1 to 4.8). No client (4.9). No push wake (4.7). |
| 5. Acquisition plugin | Core seams only: request table, endpoints, kill switch, suggestions. No worker, no repo. |
| 6. Reach and distribution | Not started. |

Concrete state of the server today: 50 routes in `internal/api/server.go`, 18
internal packages, migrations `0001` to `0017`, tests green, Docker image
written but never built.

## 2. Work that should happen next, in order

1. `[CRIT]` Enforce the per-device tokens the registry already issues. The API
   has no authentication at all, and the Docker image listens on `0.0.0.0:8080`.
2. `[CRIT]` Encrypt connector tokens at rest. Spotify access and refresh tokens
   are stored in plaintext in `connector_token`.
3. `[BLOCK]` `[OWNER]` Decide gate G3 (client stack) so 4.9 can start.
4. Finish the Spotify OAuth path: PKCE helper, token exchange, refresh.
5. Harden the server pieces Phase 4 assumes: rate-limit enforcement on every
   connector, file stability window, download resume verification.
6. Add the missing quality rails: property tests, simulation harness, fuzz
   targets, secret scanning and linting in CI.
7. Build the client skeleton and the 50k-track benchmark.
8. Push wake (ntfy, UnifiedPush, WorkManager).
9. Ops: compose file, backups with restore test, monitoring.
10. Distribution plumbing: signed releases, attestations, store registrations.

## 3. Owner decisions and inputs

### 3.1 Decisions

- [ ] `[OWNER]` `[BLOCK]` **D7 / gate G3: client stack.** Handbook weighs KMP 83
      vs Flutter 71 (CLI-1); the roadmap leans Flutter. Decide only after the
      desktop spike (section 5.1). Write an ADR with the matrix.
- [ ] `[OWNER]` **D8b: import Spotify extended streaming history?** Default no,
      weight 0, local-only table. Record the answer in the roadmap.
- [ ] `[OWNER]` Confirm the home-server assumption (D9) and the scale,
      personal or small group. Everything about rate limits and hosting assumes
      a home connection.
- [ ] `[OWNER]` Confirm DCO is final and that core stays closed to outside PRs
      until there is a policy for accepting contributions.
- [ ] `[OWNER]` Syncthing: recommended companion or merely tolerated? Current
      default is tolerated, external mode must stay safe either way.
- [ ] `[OWNER]` `[LATER]` Decide whether `docs/` is ever published. If yes, the
      roadmap's D3 must be rewritten from GPL to the AGPL that shipped.
- [ ] `[OWNER]` Confirm the project name is clear: Play Store search, F-Droid
      search, trademark search for "GrooveSync". Fallbacks: Groovekeep, Waxkeep.

### 3.2 Accounts, keys and credentials the code cannot provide

- [ ] `[OWNER]` Android release keystore, two offline copies, fingerprint stored
      outside the repo. Losing it forces every user to uninstall.
- [ ] `[OWNER]` Android developer verification: package name plus signing key.
      Free limited distribution now, full registration before public release.
- [ ] `[OWNER]` Spotify: a test account with Premium and its own Dev Mode client
      ID, for live tests only. Never commit it.
- [ ] `[OWNER]` YouTube Music: a throwaway Google account plus the `ytmusicapi`
      sidecar, for live tests only.
- [ ] `[OWNER]` Navidrome: `SYNCD_NAVIDROME_USER` and password, or an explicit
      decision to keep live mirror tests skipped.
- [ ] `[OWNER]` ListenBrainz token, or confirm the connector stays mock-tested.
- [ ] `[OWNER]` AcoustID: whether to support per-user keys, and whether to submit
      fingerprints at all. Default: lookups only, own key optional.
- [ ] `[OWNER]` Hand-label the identity golden set: a few hundred awkward tracks
      (compilations, live, remasters, multi-artist, cue-sheet albums). Until then
      the resolver tests use synthetic fixtures, which do not prove match quality.
- [ ] `[OWNER]` Tailscale (or another mesh) on every device plus the Docker host,
      if the LAN-only path is not enough.

## 4. Phase 4, server side

### 4.1 Authentication and the API boundary `[CRIT]`

The registry issues a token per device and stores only `token_hash`, but no
handler checks it. `README.md` says so. The Docker image sets
`SYNCD_ADDR=0.0.0.0:8080`.

- [ ] Add middleware that requires `Authorization: Bearer <device token>` on
      every `/sync/v1/*` route except `meta`, `about` and `healthz`.
- [ ] Separate admin authority for device management (`/sync/v1/devices*`,
      settings, connector actions). A device token must not register or delete
      other devices.
- [ ] Constant-time token comparison, per-device lookup by hash, 401 with no
      detail on failure.
- [ ] Rate-limit registration and failed auth attempts.
- [ ] Tests: missing token, wrong token, deleted device, admin-only route with a
      device token.
- [ ] Document the two credentials in the README and in the client setup flow.
- [ ] Until auth ships, change the image default back to `127.0.0.1` and say why
      in the README. Do not publish an image that listens on all interfaces
      without auth.

### 4.2 Connector secrets `[CRIT]`

- [ ] Encrypt `connector_token` values at rest: AES-GCM with a key from the
      environment, versioned ciphertext, migration for existing rows.
- [ ] Never log token values. Add a redaction test around the slog handler.
- [ ] Delete Spotify tokens and Spotify-derived rows on disconnect, and test the
      5-day rule from R-D1.
- [ ] YouTube cookies are a full login: store locally, per user, never logged.
      The sidecar file path must be outside the repo and outside a backup that
      leaves the house.

### 4.3 Spotify OAuth path

The API client, write-back, import and re-auth state machine exist. The
authorization flow does not.

- [ ] PKCE authorization-code flow with a loopback IP literal redirect, no
      client secret.
- [ ] Desktop helper that runs on the machine with a browser and hands tokens to
      the vault (a headless server cannot receive a loopback redirect).
- [ ] Store `authorized_at`, expose the 6-month countdown, and drive the existing
      connected -> expiring -> reconnect-required -> disconnected states.
- [ ] Drop the token and stop retrying on `invalid_grant` (handler exists, wire
      it to real refresh failures).
- [ ] Live test against the owner's test app, recorded once.

### 4.4 Sync protocol and file handling

Most of 4.2 and 4.4 exists. Verify, then close the gaps.

- [ ] Verify resumable download honours `Range` and only resumes on 206.
- [ ] Verify upload chunk verification rejects a bad digest and leaves no final
      file behind.
- [ ] Add a stability window before indexing or hashing: size and mtime unchanged
      over a short interval. `hashFile` already re-hashes if a file changes
      during the read; a half-written file still needs the pre-check.
- [ ] Re-hash on change and record a new rendition when the same path yields a
      different hash (4.5: keep both until the user chooses).
- [ ] Confirm the manifest diff is deterministic and covered by tests.
- [ ] Confirm the same content arriving by app sync and by Syncthing is stored
      once (4.8 mixed fleets).
- [ ] Standard layout check: the mirror writes `Artist/Album/...`; prove the
      database is rebuildable from the folder with a test that drops all rows
      and re-runs the mirror.

### 4.5 Taste-driven storage (4.6)

- [ ] The planner emits eviction suggestions. Add download planning by affinity
      threshold, policy cap and network rule.
- [ ] Never plan an eviction of a liked or recently played track; add the test.
- [ ] In `external` mode, returns suggestions only, never deletes. Test it.
- [ ] Decide how "recently played" is derived from the event log and document it.
- [ ] Auto-download execution belongs to the client. Document the contract.

### 4.6 Wake and push (4.7)

- [ ] SSE exists. Keep it for the web UI and desktop.
- [ ] Android: WorkManager poll baseline, UnifiedPush when a distributor exists,
      ntfy (`Apache-2.0` arm) as the default push, per-user topic. No FCM in the
      `foss` flavour.
- [ ] Server side: publish a mutation event to ntfy after library, like, playlist
      and acquisition changes. Make it opt-in and configurable.
- [ ] Do not build push before the client exists.

### 4.7 External mode (4.8)

- [ ] Verify every rule with a test: no auto-delete, no mirror-deletes, no
      affinity rules, whole-folder scope only.
- [ ] Ignore list is configurable (`SYNCD_IGNORE_GLOBS`); include
      `.stfolder`, `.stversions`, `.syncthing.*.tmp` by default.
- [ ] `.sync-conflict-` copies go to the review queue, never the library. Done;
      add a test with a real Syncthing filename.
- [ ] Holdings report covers files that arrived by Syncthing, not only uploads.
- [ ] Do not write tags into files on external devices.

### 4.8 Observability (5.6)

- [ ] Health payload: add resolver backlog, MusicBrainz and AcoustID queue depth,
      review-queue size, write-back mode plus last batch, plugin status and
      acquisition provider state.
- [ ] Connector states must distinguish connected, degraded, reconnect-required.
- [ ] Keep `/sync/v1/health` cheap: no outbound calls beyond the existing probes.

## 5. Gate G3 and the first-party client (4.9)

### 5.1 Decide the stack

- [ ] `[OWNER]` `[BLOCK]` Three-day desktop audio spike behind a `PlayerEngine`
      interface: libmpv first, vlcj second. Record pass/fail on gapless,
      ReplayGain and MPRIS or SMTC.
- [ ] If the spike fails, take CLI-5: Flutter + `just_audio` +
      `just_audio_media_kit`.
- [ ] Write ADR-0002 with the matrix and the spike result.

### 5.2 Skeleton and performance baseline

- [ ] Client repo or module layout: `android`, `desktop`, `common`, `spec`.
- [ ] HLC implementation plus the shared golden vectors. Go and the client must
      pass the same JSON file.
- [ ] Thin OpenSubsonic client (about 8 calls; do not take the GPL Go client).
- [ ] Room 2.8.5 versus SQLDelight 2.4.1 on a 50k-track fixture, then pick.
- [ ] Metadata mirror sync and the 50k list benchmark immediately.
- [ ] Cold start and jank budgets from UX-4, wired into Macrobenchmark.

### 5.3 Playback

- [ ] Media3 `MediaLibraryService`, one service feeding UI, notification, lock
      screen and Android Auto later.
- [ ] `mediaPlayback` foreground service; `dataSync` only for user-started
      downloads, with `onTimeout`; never from `BOOT_COMPLETED`.
- [ ] Play reporting: `scrobble` and `playbackReport`, `ignoreScrobble` for
      now-playing, and a stream must not create a play.
- [ ] Gapless and ReplayGain per the spike result.
- [ ] Local files play without the server; offline badges per UX-1.

### 5.4 Sync client

- [ ] Outbox with HLC-stamped ops, idempotent push, cursor pull.
- [ ] Resumable downloader on OpenSubsonic `download`: `.part`, Range on 206,
      size and content-type check, then verify.
- [ ] Per-device pin policy and manifest diff.
- [ ] Conflict handling: plays append-only, ratings and pins LWW, playlists
      whole-list LWW with a conflict copy.
- [ ] Eviction order: unpinned, then least recently played, then lowest affinity.
- [ ] Ordered server URL list, LAN first, mesh second, public last.
- [ ] `external` mode: folder grant (SAF), watch, hash, report holdings, never
      move or delete.

### 5.5 UI and taste

- [ ] Five destinations: Home, Search, Library, Offline, You.
- [ ] Capability flags and disabled controls with a one-line reason (UX-3).
- [ ] Taste ring with five levels; numeric score only in Stats.
- [ ] "Why" sheet built from real score contributions, plus a manual override
      that always wins and survives re-sync and reinstall.
- [ ] Review queue and conflict copy screens.

### 5.6 Tests and telemetry

- [ ] Roborazzi screenshots, about five Maestro flows, nightly Firebase Test Lab
      smoke.
- [ ] Audio tests at decoder level with synthetic sine and timecode files.
- [ ] v0 crash reporting is none. Add a user-initiated, scrubbed diagnostics
      export. v1 opt-in self-hosted GlitchTip only.

### 5.7 Exit criteria for Phase 4

- [ ] A file added on device A appears on device B per B's policy, with an event
      trail.
- [ ] Manual folder copy re-indexes correctly by hash.
- [ ] Eviction never removes a liked or recently played track.
- [ ] An `external` device shows in holdings, gets no automatic deletions, and
      its `.sync-conflict-` copy lands in the review queue.
- [ ] The same device works over LAN and over a mesh with only a URL change.
- [ ] Airplane mode for a week loses no plays or ratings.

## 6. Phase 5, acquisition plugin (separate repo)

- [ ] `[OWNER]` Create the separate repo, neutral name, no YouTube branding,
      its own licence.
- [ ] Core stays clean: the plugin talks only through `acquisition_request` and
      the normal file ingest path. Add a test that the core builds and passes
      with the plugin absent.
- [ ] Worker: `yt-dlp` as an external executable on the nightly channel, not
      embedded extraction code.
- [ ] Re-run the yt-dlp health check at startup: JS runtime, PO-token provider,
      cookies. Surface "YouTube downloads unavailable" in the health payload.
- [ ] Provider interface with one implementation. A second backend only if
      yt-dlp proves unreliable.
- [ ] Requests carry MusicBrainz metadata, never Spotify content.
- [ ] Prefer audio-track items over music videos and uploads; keep alternates
      with a confidence flag.
- [ ] Downloaded files become `cached` renditions, ranked below local lossless.
- [ ] Queue: serialised, 5 to 10 seconds between downloads, daily cap, no
      "download my whole library" default.
- [ ] Kill switch clears the queue (exists in core; wire it to the worker).
- [ ] Home connection only; never a VPS or cloud host.
- [ ] Throwaway Google account only; cookies stored locally and never logged.
- [ ] Failure isolation: provider down shows a clear state and nothing else
      degrades.

## 7. Phase 6, reach and distribution

### 7.1 Linux

- [ ] AppImage or tar plus SHA-256 and attestation per release.
- [ ] Flathub: human-written manifest, never generated. Offline build, app-code
      AI disclosure in the PR, `--own-name=org.mpris.MediaPlayer2.$FLATPAK_ID`,
      session-bus access. AI agents must not open the PR or write its messages.
- [ ] AUR source `PKGBUILD` after tagged releases; `-bin` later.
- [ ] `.deb` and `.rpm` as release assets. No Snap.

### 7.2 Windows

- [ ] Build with the chosen toolkit.
- [ ] Unsigned zip or installer with SHA-256 and attestation first.
- [ ] SignPath Foundation once there is public history and one release.
- [ ] MSIX for the Microsoft Store as the trusted channel later.

### 7.3 macOS

- [ ] Ad-hoc sign with `codesign -s -`, DMG with "Open Anyway" steps at the top
      of the README.
- [ ] Own or community Homebrew tap. Not the official cask.
- [ ] Notarisation only if the audience demands it, about $99/year.

### 7.4 iOS

- [ ] Primary route: document OpenSubsonic clients against the server. No work
      beyond the README.
- [ ] Optional sideload IPA: 7-day signature, 3-app cap, no push. Say so.
- [ ] Watch the Australia window (1 to 15 Feb 2027 stay decision, 1 Mar 2027
      start). Do not plan around it.

### 7.5 Android

- [ ] GitHub Releases first: signed per-ABI APKs, about 30 MB, SHA-256 and
      attestation. Obtainium and the F-Droid client as update paths.
- [ ] F-Droid main after at least three months public: reproducible-build route
      (a), declare `NonFreeNet`, no Play Services or Firebase, Fastlane metadata
      in the repo.
- [ ] Google Play last: `play` flavour, 12 testers for 14 continuous days, no
      self-updater, Play in-app updates.
- [ ] IzzyOnDroid is not pursued. Its policy bans AI-generated code.

### 7.6 Releases and updates

- [ ] Conventional Commits plus release-please; build inside the same workflow
      gated on `release_created`, or use an App token.
- [ ] SBOM with syft and provenance with `actions/attest`. Document
      `gh attestation verify`.
- [ ] Stable and beta feeds. No in-app self-updater on any channel.
- [ ] Three independent keys: Android keystore, desktop updater, optional
      Authenticode. Document rotation.

### 7.7 Store and legal paperwork

- [ ] `[OWNER]` Privacy policy.
- [ ] `[OWNER]` Terms and DMCA/ToS position for connectors.
- [ ] `[OWNER]` Content-rights answers for Play and the Microsoft Store.
- [ ] Keep `AI-POLICY.md` consistent with what each store was told.
- [ ] Fastlane metadata, screenshots, changelogs per channel.

## 8. Cross-cutting quality

### 8.1 Tests

- [ ] Golden identity set from the owner, run on every resolver change.
- [ ] Shared JSON merge fixtures already exist; add property tests with `rapid`
      for idempotence and the order-independence the algorithm claims.
- [ ] Simulation harness: 3 to 5 replicas, drop, duplicate, reorder, partition,
      clock jump, crash-restart. 200 seeds per PR, 10,000+ nightly, seed printed
      on failure.
- [ ] Fault injection: expired Spotify token, ytmusicapi parse failure,
      MusicBrainz 503, AcoustID 429, Navidrome down, companion down.
- [ ] Fuzz targets: tag parse (once TagLib lands), cover-art extraction, sync op
      decoder. Commit crashers as seeds.
- [ ] Contract tests: vendor the OpenSubsonic OpenAPI, checksum it, validate
      every response, record goldens, nightly against `latest` Navidrome.
- [ ] Patch-coverage gate of 80% on sync, parsers, migrations and affinity, not
      a global number.
- [ ] Flaky policy: no auto-retry on PR gates, one logged E2E retry, quarantine
      after two flakes in 14 days.
- [ ] Benchmark `modernc.org/sqlite` write throughput on ARM before claiming a
      Pi deployment.

### 8.2 CI

Current `ci.yml` runs build, vet and test only.

- [ ] Add `golangci-lint`, `actionlint`, `zizmor`, `hadolint` (GPL tools, dev
      time only).
- [ ] Add `gitleaks` to CI and to the local pre-commit hook.
- [ ] Add `make check-licenses` to CI. The script needs `go-licenses` on PATH;
      add a bootstrap step or `go run` so it does not fail on a clean machine.
- [ ] Dependabot with an explicit 14-day cooldown.
- [ ] Branch protection on `main`.
- [ ] Nightly job for the contract and simulation suites.

### 8.3 Security

- [ ] Secret scanning (above).
- [ ] Threat model for the companion: LAN, tailnet, public opt-in, token theft.
- [ ] Treat every Spotify, ytmusicapi and Navidrome response as untrusted input.
- [ ] `SECURITY.md` with a contact and a disclosure policy.
- [ ] Backups: restic of Navidrome and syncd data, second disk plus one offsite
      target, quarterly restore test.
- [ ] Monitoring: Uptime Kuma plus ntfy.
- [ ] Confirm no database, `.env`, key or PEM file is ever tracked; the current
      tree is clean.

### 8.4 API contract

- [ ] `/sync/v1/meta` with proto, `features[]`, `min_build`; `X-Client-*`
      headers; 426 responses; server-first updates (OPS-3).
- [ ] Keep proto N and N-1 working.
- [ ] Publish the compatibility rule in the README.

## 9. Leftovers from phases 1 to 3

- [ ] `[OWNER]` Decide whether a web UI is still in scope (1.5). Nothing exists
      today; the API is the whole product surface.
- [ ] If yes: Connections, Library, Likes, Review queue, Health. Setup wizards
      that explain the BYO Spotify app and the Premium requirement.
- [ ] Native TagLib scanner (1.2). Today the library comes from Navidrome, which
      means the service cannot run standalone in Branch B.
- [ ] Weekly live contract tests for Spotify and YouTube (2.5).
- [ ] Score "why" view over HTTP: `Contributions` exist in the model; confirm the
      endpoint exposes them.
- [ ] Manual score override with storage and survival across re-sync.
- [ ] Cold start is implemented as a function; wire it into scoring and test it
      with local-only tracks.
- [ ] A test that fails if any Spotify-sourced field reaches score inputs (D8).

## 10. Ops and home-server setup

- [ ] `compose.yml` for Navidrome plus syncd, pinned by tag and digest, loopback
      ports, `cap_drop: ALL`, read-only music mount, `user: "1000:1000"`.
- [ ] Document the Navidrome 0.64 ID migration and the irreversible mapping.
- [ ] Document backup, restore and upgrade order (back up, pull, up, check
      `/sync/v1/meta`).
- [ ] Diun notify-only, no Watchtower.
- [ ] Document sleep, wake-on-LAN and UPS expectations.
- [ ] Document the ordered URL list and the Tailscale setup.

## 11. Documentation and repo hygiene

- [x] `LICENSE` is AGPL-3.0-or-later, one file, sha256 `d8a6cc31...bee`.
- [x] README licence section, GPL-to-AGPL note, Docker and source-URL notes.
- [x] CONTRIBUTING with DCO and the SPDX helper.
- [x] THIRD_PARTY_NOTES and the bundled licence tree.
- [x] SPDX headers on Go, shell, Python and SQL sources.
- [ ] `docs/pins.md`: every pin with version, release date, evidence URL and the
      date checked.
- [ ] ADR-0001 branch A vs B (G0). ADR-0002 client stack (G3).
- [ ] CHANGELOG generated from Conventional Commits.
- [ ] Rewrite the roadmap's D3 to AGPL if `docs/` is ever published.
- [ ] Decide whether `docs/` stays local. It is ignored today.
- [ ] Keep this file current. It is the single list of open work.

## 12. Definition of done for the project

- [ ] Phase 4 exit criteria all pass with a real client on two devices.
- [ ] Phase 5 is either shipped in its own repo or explicitly dropped.
- [ ] A new user can install from GitHub Releases, F-Droid and Flathub without
      reading a build guide.
- [ ] Every channel's update path is documented and none ships an in-app
      self-updater.
- [ ] Android verification registration is done before the first public release.
- [ ] The AI-code stance is published and consistent across channels.
- [ ] Privacy policy, security policy and DMCA/ToS position are published.
- [ ] Backups restore cleanly on a machine that did not create them.
- [ ] The licence story is accurate everywhere: AGPL now, GPL for the pre-2026-10-31
      snapshot, sole copyright holder.

## 13. Out of scope, do not build

- YouTube streaming in core. No compliant route exists.
- Spotify audio playback. No API provides it.
- Apple Music connector in v1.
- Spotify Connect remote control.
- Social features, Listen Along, MCP control surface.
- An in-app APK self-updater.
- Bundling or embedding Syncthing.
- NewPipeExtractor.
- A native iOS App Store release.

## 14. Dates that force work

| Date | Event | Action |
|---|---|---|
| Rolling, about every 6 months per user | Spotify refresh-token expiry | Reconnect flow must exist before Spotify ships. |
| Before each Navidrome upgrade | Plugin and API still moving; 0.64 ID migration is irreversible | Re-check the release, pin tag and digest, back up `/data`. |
| 1 to 15 Feb 2027 | Apple may defer Australia's orders to 3 May 2027 | Only if iOS is attempted. |
| 1 Mar 2027 | Australia alternative stores and direct downloads | Optional iOS route. |
| 2027, no firm date | Android developer verification goes global | Full registration before public distribution. |
| 1 Aug 2027 | Old MusicBrainz OAuth and digest auth stop | Use metabrainz.org with PKCE for authenticated calls. |

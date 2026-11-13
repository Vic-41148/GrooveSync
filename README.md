# GrooveSync

GrooveSync is a local-first music app with a Go companion service named `syncd`.

Current scope:
- `main` holds the stable working line
- `dev` holds active work
- `docs/` stays local-only and is ignored by git

## Quick start

```sh
go build ./...
go vet ./...
go test ./...
```

## Service

`server/cmd/syncd` starts the companion HTTP service.

The service currently exposes:
- `GET /healthz`
- `GET /sync/v1/meta`
- `GET /sync/v1/about` - returns the published source URL and running version

`SYNCD_SOURCE_URL` sets the source URL reported by `/sync/v1/about`. It defaults
to the canonical repository and exists so operators of a fork or a modified
build can point users at the corresponding source, as AGPL section 13 requires.

## Device sync modes

- `app`: the GrooveSync app owns the device folder and syncs over the LAN (default for new devices)
- `external`: an external tool owns the folder, for example Syncthing

New devices default to `app`. To opt in to `external`, send
`"sync_mode":"external"` in the registration payload. Omitting `sync_mode`
during registration keeps the mode already stored for an existing device; send
an explicit mode to change it.

In `app` mode a device advertises LAN URLs that other devices connect to. The
server listens on the loopback address by default and requires a bearer token
on every `/sync/v1` route, including device registration. Set
`SYNCD_ADMIN_TOKEN` to the owner's token; devices then register with that token
and receive their own per-device token. The server refuses to start on a
non-loopback address when `SYNCD_ADMIN_TOKEN` is empty, so an exposed instance
is always authenticated. Keep the instance behind a VPN such as Tailscale.

## Compatibility

`GET /sync/v1/meta` is the handshake. It returns:

- `proto`: the wire protocol the server speaks.
- `proto_min`: the oldest protocol the server still accepts. A bump keeps
  `proto - 1` readable for at least 90 days.
- `features[]`: behaviour switches. A client must not infer a feature from the
  version alone.
- `min_build` and `recommended_build`: per-platform build floors.
- `schema`: the database schema version.

The rules:

- `proto` changes only on a breaking wire change. Additive fields and new
  features do not bump it.
- A client older than `proto_min` gets `426 Upgrade Required` and keeps local
  playback working. The server reads `X-Client-Proto`, `X-Client-Platform` and
  `X-Client-Build` and enforces `proto_min` and `min_build`.
- A client newer than the server is read-only until the server is updated.
- The server is updated first. A client must tolerate a server one protocol
  behind.

The client sends those headers on every `/sync/v1` request. Requests without
them are accepted for backwards compatibility, and every response carries
`X-Server-Proto`.

## Docker

The image is multi-stage: a Go builder stage and a distroless static runtime
with no shell or package manager. The service writes its SQLite database to
`/data/syncd.db` and runs as the nonroot user (UID 65532).

```sh
export SYNCD_ADMIN_TOKEN="$(openssl rand -hex 32)"
docker build -t groovesync:dev .
docker volume create groovesync-data
docker run --rm -d --name groovesync \
  -p 127.0.0.1:8080:8080 \
  -e SYNCD_ADMIN_TOKEN \
  -v groovesync-data:/data \
  groovesync:dev
curl -sS -H "Authorization: Bearer $SYNCD_ADMIN_TOKEN" \
  http://127.0.0.1:8080/sync/v1/about
docker stop groovesync
```

A named volume inherits the ownership of `/data` from the image, so the nonroot
user can write without extra steps. A bind mount does not: prepare it with
`mkdir -p data && chown -R 65532:65532 data` first.

The container listens on all interfaces (`SYNCD_ADDR=0.0.0.0:8080`) because the
loopback default is unreachable from outside the container, and it refuses to
start without `SYNCD_ADMIN_TOKEN`. The example publishes only to the host
loopback. Publishing a LAN address is safe only with the token set and the
instance behind a VPN.

`docker-compose.yml` runs Navidrome and `syncd` together. Export the admin token
and Navidrome credentials first:

```sh
export SYNCD_ADMIN_TOKEN="$(openssl rand -hex 32)"
export SYNCD_NAVIDROME_USER=admin
export SYNCD_NAVIDROME_PASSWORD=changeme
docker compose up -d --build
```

`syncd` keeps its database in the `groovesync-data` named volume, mounts the
music folder read-only at `/music`, reaches Navidrome at `http://navidrome:4533`
and publishes only to loopback.

## Backups

`scripts/backup.sh` snapshots the Navidrome data directory, the syncd database
and the compose inputs into a restic repository, then prunes and checks it. Set
`RESTIC_REPOSITORY` and `RESTIC_PASSWORD_FILE` first and run `make backup`.
`ops/systemd/` carries a daily timer. Restore and upgrade order live in the
local ops notes.

`docker compose --profile monitoring up -d` adds Uptime Kuma on loopback and
notify-only Diun. Set `DIUN_NOTIF_NTFY_TOPIC` first. Diun watches containers
labelled `diun.enable=true` and never updates anything.

## Desktop client setup

The desktop client lives in `desktop/` and is built with Gradle.

```sh
./gradlew :desktop:build :desktop:test
```

Run the CLI from Gradle or from a distribution:

```sh
./gradlew :desktop:run --args="meta --data DIR"
./gradlew :desktop:installDist
./desktop/build/install/desktop/bin/desktop status --data DIR
```

Pinned tool and dependency versions are recorded in `docs/pins.md` (local-only).

## License

This project is licensed under the GNU Affero General Public License v3 or later (AGPL-3.0-or-later). See LICENSE for the full text. By contributing, you agree to sign-off your commits using the Developer Certificate of Origin (DCO). For details on contribution and DCO sign-off see CONTRIBUTING.md.

Source published on the dev branch before 2026-10-31 carried a GPL-3.0 LICENSE file. Those copies remain available under GPL-3.0. This project is AGPL-3.0-or-later from that date forward.

Third-party licenses and attribution

This repository includes third-party dependencies licensed under permissive licenses (MIT, BSD-2/3-Clause, Apache-2.0, etc.). A bundled copy of dependency license texts and notices is included at /usr/share/licenses/groovesync/ inside the project (and the Docker image). See third_party_licenses/ for details.

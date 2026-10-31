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

`cmd/syncd` starts the companion HTTP service.

The service currently exposes:
- `GET /healthz`
- `GET /sync/v1/meta`
- `GET /sync/v1/about` - returns the published source URL and running version

## Device sync modes

- `app`: the GrooveSync app owns the device folder and syncs over the LAN (default for new devices)
- `external`: an external tool owns the folder, for example Syncthing

New devices default to `app`. To opt in to `external`, send
`"sync_mode":"external"` in the registration payload. Omitting `sync_mode`
during registration keeps the mode already stored for an existing device; send
an explicit mode to change it.

In `app` mode a device advertises LAN URLs that other devices connect to. The
service has no authentication layer yet, so anyone who can reach those URLs can
reach the sync endpoints. Keep it on a trusted network or behind a VPN such as
Tailscale, and do not expose it to the internet.

## License

This project is licensed under the GNU Affero General Public License v3 or later (AGPL-3.0-or-later). See LICENSE for the full text. By contributing, you agree to sign-off your commits using the Developer Certificate of Origin (DCO). For details on contribution and DCO sign-off see CONTRIBUTING.md.

Third-party licenses and attribution

This repository includes third-party dependencies licensed under permissive licenses (MIT, BSD-2/3-Clause, Apache-2.0, etc.). A bundled copy of dependency license texts and notices is included at /usr/share/licenses/measure/ inside the project (and the Docker image). See third_party_licenses/ for details.

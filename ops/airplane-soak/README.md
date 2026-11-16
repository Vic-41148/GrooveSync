# Airplane-mode outbox soak

Runs the shared `SyncRunner` for seven days with the server URL pointed at a
dead endpoint. Plays, skips and likes accumulate in the outbox while every sync
attempt fails. When the deadline passes the service restores the live URL,
drains the outbox, and compares the recorded event ids with
`GET /sync/v1/events`, so loss and duplicates both fail the run.

State lives in `/srv/airplane-soak`, never in `/tmp`. The systemd unit uses
`Restart=always`, so a reboot resumes the same deadline and event log.

This exercises the shared client, not Android process death.

## Prerequisites

- A systemd host with Python 3.8 or newer.
- At least one track in the server library. Events reference a canonical id and
  the server rejects events for unknown tracks, so prepare picks a real one.
- A JDK with `jlink` and `./gradlew :desktop:installDist` on the build machine.

## Build the bundle

```
./gradlew :desktop:installDist
jlink --add-modules java.base,java.sql,java.logging,java.naming,jdk.crypto.ec,jdk.unsupported \
  --output /tmp/airplane-soak/jre
mkdir -p /tmp/airplane-soak/gsclient /tmp/airplane-soak/scripts
cp -a desktop/build/install/desktop/. /tmp/airplane-soak/gsclient/
cp ops/airplane-soak/record.py ops/airplane-soak/progress.py \
  ops/airplane-soak/set-url.py ops/airplane-soak/verify.py \
  ops/airplane-soak/prepare.sh ops/airplane-soak/run-soak.sh \
  ops/airplane-soak/airplane-soak.service ops/airplane-soak/airplane-soak.tmpfiles \
  ops/airplane-soak/soak.env.example \
  /tmp/airplane-soak/scripts/
chmod 0755 /tmp/airplane-soak/scripts/*.sh
tar -C /tmp/airplane-soak -czf airplane-soak.tar.gz gsclient jre scripts
```

## Install on the server

Run as root on `vic-home-server`. Copy `airplane-soak.tar.gz` to the host first.

```
install -d /opt/groovesync-soak
tar -xzf airplane-soak.tar.gz -C /opt/groovesync-soak
install -m 0644 /opt/groovesync-soak/scripts/airplane-soak.service /etc/systemd/system/
install -m 0644 /opt/groovesync-soak/scripts/airplane-soak.tmpfiles /etc/tmpfiles.d/airplane-soak.conf
systemd-tmpfiles --create /etc/tmpfiles.d/airplane-soak.conf
install -m 0600 /opt/groovesync-soak/scripts/soak.env.example /etc/groovesync-soak.env
```

Edit `/etc/groovesync-soak.env` if the paths or timings differ.

## Install without a bundled JRE

A host that already runs Java 21 can skip `jlink` and the `jre` tree. The soak
commands use `state.json` rather than SQLite, so the 12 MB
`lib/sqlite-jdbc-*.jar` can also be dropped, which keeps the upload near 3 MB.

```
./gradlew :desktop:installDist
mkdir -p /tmp/airplane-soak/scripts
cp -a desktop/build/install/desktop /tmp/airplane-soak/gsclient
rm -f /tmp/airplane-soak/gsclient/lib/sqlite-jdbc-*.jar
cp ops/airplane-soak/record.py ops/airplane-soak/progress.py \
  ops/airplane-soak/set-url.py ops/airplane-soak/verify.py \
  ops/airplane-soak/prepare.sh ops/airplane-soak/run-soak.sh \
  /tmp/airplane-soak/scripts/
chmod 0755 /tmp/airplane-soak/scripts/*.sh
tar -C /tmp/airplane-soak -czf airplane-soak.tar.gz gsclient scripts
```

Point `JAVA_HOME` at the host runtime in `/etc/groovesync-soak.env`. If the
host has no `groovesync` account, keep the unit generic and add a drop-in:

```
install -d /etc/systemd/system/airplane-soak.service.d
cat > /etc/systemd/system/airplane-soak.service.d/user.conf <<'EOF'
[Service]
User=vic41148
Group=vic41148
EOF
systemctl daemon-reload
```

Create `/srv/airplane-soak` owned by that account instead of using the
tmpfiles rule.

## Prepare once

The prepare step registers a fresh device with the admin token, picks the first
library track, writes the live URL, and rewrites the state to the dead URL. It
refuses to run if a state file already exists. Use a different `SOAK_DIR` to
keep several runs side by side. Set `SOAK_TRACK` to a known canonical id to pin
a specific track instead of the first one.

```
read -rsp 'admin token: ' SYNCD_ADMIN_TOKEN; echo
sudo env SYNCD_ADMIN_TOKEN="$SYNCD_ADMIN_TOKEN" \
  SOAK_SERVER_URL=http://127.0.0.1:8080 \
  /opt/groovesync-soak/scripts/prepare.sh
unset SYNCD_ADMIN_TOKEN
```

The admin token is read through `--admin-env`, so it never appears in the
client argument list. The device token stays in `state.json`, mode 0600.

## Start

```
systemctl daemon-reload
systemctl enable --now airplane-soak
```

## Check progress

```
systemctl status airplane-soak
cat /srv/airplane-soak/daily.csv
cat /srv/airplane-soak/status.json
journalctl -u airplane-soak -n 50 --no-pager
```

`daily.csv` has one row per UTC day with enqueue counts. `status.json` shows the
current outbox depth and the deadline.

## After the deadline

The service restores the live URL, retries sync until the outbox is empty, then
writes `/srv/airplane-soak/verify.json` and `done`. A successful run has
`"ok": true`, `missing` and `unexpected` empty, `duplicates` zero, and
`outbox` zero.

The service stays active and idle after that. Stop it with
`systemctl disable --now airplane-soak`.

## Reset

```
systemctl disable --now airplane-soak
rm -rf /srv/airplane-soak
```

Register a new device id for the next run. Never reuse a device id with a fresh
state, because the verifier would see the older events as unexpected.

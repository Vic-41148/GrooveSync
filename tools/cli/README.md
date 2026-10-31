GrooveSync minimal CLI client

This is a tiny helper used during development to exercise the Spotify PKCE
flow and the file sync endpoints against a local syncd server. It is not a
production client.

Auth:

  go run ./tools/cli -server http://127.0.0.1:8080 -account acct-1 -open

The program calls the server's start-auth endpoint, prints the authorization
URL, and optionally opens it in the desktop browser. If the server cannot be
reached for the callback, paste the code parameter returned by Spotify into the
prompt to complete the flow via POST /sync/v1/connectors/spotify/{account}/complete-auth.

Upload:

  go run ./tools/cli -server http://127.0.0.1:8080 \
    -mode upload -file track.flac -token "$SYNCD_ADMIN_TOKEN" -chunk-size 4194304

The file is hashed locally, uploaded in parts, indexed as a file rendition,
downloaded back and compared by SHA-256. The server needs SYNCD_UPLOAD_DIR set
and SYNCD_ADMIN_TOKEN unless it is bound to loopback. Pass -track to attach the
file to an existing canonical track; without it the server creates a
provisional track that identity resolution can merge later.

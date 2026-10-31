GrooveSync minimal CLI client

This is a tiny helper used during development to exercise the Spotify PKCE
flow against a local syncd server. It is not a production client.

Usage:

  go run ./client/cli -server http://127.0.0.1:8080 -account acct-1 -open

The program calls the server's start-auth endpoint, prints the authorization
URL, and optionally opens it in the desktop browser. If the server cannot be
reached for the callback, paste the code parameter returned by Spotify into the
prompt to complete the flow via POST /sync/v1/connectors/spotify/{account}/complete-auth.

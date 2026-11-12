# reachability

Development probe for the phase 0.7 reachability spike. It exercises syncd's
file routes the way a client would: generate a file, upload part of it, check
the offset the server reports, resume from that offset, download the file and
compare SHA-256.

    SYNCD_ADMIN_TOKEN=... go run ./tools/reachability \
      -server https://vic-home-server.tailac870b.ts.net:10000

Flags:

- `-size` and `-stop-after` are byte counts. The defaults are 100 MiB and
  32 MiB.
- `-chunk` is the upload chunk size, 4 MiB by default.
- `-device-id` sends `X-Device-Id` for a device token; the admin token does not
  need it.
- `-workdir` and `-keep` control where the generated and downloaded files live.
- `-timeout` bounds the whole run.

The probe stops between chunks rather than aborting a TCP stream mid-body, so
the server's part file is intact and the resume path is the one a client uses.
It prints the hash it uploaded; clean the matching rows and files on the server
afterwards.

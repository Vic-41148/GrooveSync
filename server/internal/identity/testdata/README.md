# Identity test data

`golden.json` holds 18 hand-written awkward cases: ISRC and MBID conflicts,
remaster suffixes, live and acoustic versions, duration drift, artist prefixes
and no-match cases.

`youtube_playlist.json` is a metadata-only fixture derived from the playlist the
owner supplied:

    https://www.youtube.com/playlist?list=PLB5Ac5TbLc2OHUC5uaAuFdbxMXQK3ZaAF

It was fetched on 2026-11-12 with:

    yt-dlp --flat-playlist --dump-single-json --no-warnings '<playlist url>'

No audio was downloaded. The playlist titles were cleaned into artist and title
pairs, and the fixture adds exact, remastered, live-only, duration-drift and
wrong-artist variants around 110 real tracks. The labels are derived from the
entries and still need owner review; this set is a regression suite, not the
measured owner-labelled library.

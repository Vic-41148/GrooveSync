GrooveSync - Items needing your input or credentials

This file lists decisions and artifacts that require your action before the remaining
work can be completed. Keep answers concise; each line points to the responsibility
and why it is needed.

Decisions
- D3: Server license choice (AGPL or GPL)
  - Affects distribution and packaging choices. Required to finalise licensing text
    and any downstream distribution behaviour.
  - Context:
    - AGPL requires that anyone who runs a modified version over a network must
      make the source available to users of the service. This can impose
      obligations on hosted deployments and third-party forks.
    - GPL does not include the network-use clause and is less restrictive for
      service operators, but still requires distribution of source when binaries
      are distributed.
    - Choose AGPL if you want strong copyleft on networked use. Choose GPL if
      you prefer permitting networked services while preserving copyleft on
      distributed binaries.
    - Impact on this repo: the chosen license affects packaging, CI checks, and
      public distribution artifacts. I can update license files and packaging
      once you decide.

- D10: Default per-device sync mode ("app-only" or "open")
  - Determines the default device SyncMode used when new devices are registered.
    Needed to finalise API behaviour and tests that depend on default policies.
  - Context:
    - "app-only" limits device syncs so content is not automatically available to
      the wider LAN or other devices; useful for privacy-first or single-device
      setups. It favours conservative defaults for new users.
    - "open" allows devices to be reachable by other clients (peer sync), which
      is convenient for multi-device households but increases exposure and may
      require stronger network security considerations.
    - Considerations:
      - Defaulting to app-only is safer and less surprising for new users.
      - Defaulting to open simplifies multi-device sync but should be opt-in
        for security-conscious deployments.
    - I can update the default in code, docs, and tests once you pick a default.

Credentials and external resources
- Navidrome credentials or decision to skip
  - Navidrome plugin validation and live mirror tests require SYNCD_NAVIDROME_USER
    and SYNCD_NAVIDROME_PASSWORD or an explicit decision to skip live validation.

- Connector credentials for live integration tests
  - Spotify, YouTube Music, ListenBrainz, AcoustID and other live connector tests
    require API keys or account tokens. Provide credentials or confirm that live
    tests should remain skipped and mocked in CI.

Fixtures and labelling
- Hand-labelled identity regression fixture
  - A labelled fixture of identity mappings for resolver regression tests needs
    manual labelling. If you want this included, provide the CSV or confirm that
    synthetic fixtures are acceptable.

Policy and design choices
- Resumable transfer protocol and chunk policy
  - Specify the resumable transfer approach to use for device transfers so the
    transfer planner and resumable upload/download integration can be finalised.

- Runtime feature gating for connector writes
  - Decide whether remote write executors should be toggled at runtime via an
    API/setting (rollout) or only via startup configuration. This affects how
    the writeback processor is wired and tested.

Optional but recommended
- Confirm whether a dedicated background worker surface is required for
  fingerprint/backfill tasks, or whether the existing Backfill.Run invocation is
  sufficient.

How to respond
- Reply with the specific choices and any credentials you want me to use. For
  credentials, either provide them here (not recommended) or instruct where to
  mount them as environment variables or a secrets file in CI.

Once you provide the above, I will implement the remaining tasks, run the tests,
and commit changes to origin/dev with GPG-signed commits and DCO sign-off.

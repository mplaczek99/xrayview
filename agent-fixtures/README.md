# agent-fixtures

Deterministic input the agent harness feeds to the live backend. The contents
of this directory are gitignored — populate it locally before running
`npm run agent:serve`.

## Expected layout

```
agent-fixtures/
├── default.dcm    # De-identified DICOM that playwright flows open
└── out/           # Processed DICOM files get written here
```

## Rules

- Only de-identified or public DICOM samples. The repo explicitly refuses to
  host clinical data (see `CLAUDE.md` safety scope).
- `default.dcm` is the fixture the harness points `VITE_XRAYVIEW_AGENT_FIXTURE`
  at. Override by exporting the env var before launching the harness.
- `out/` is created on demand; leave it writable by the user running the
  harness.

## Smoke behaviour

If `default.dcm` is missing, the smoke flow fails with:

```
fixture not found at <path>: populate agent-fixtures/ per README and retry
```

That single message is the contract — no stack trace, no retry loop.

# Airgap outbound audit — live non-fixture (2026-08-13)

## What was run

| Probe | Mode | Result | Artifact |
|---|---|---|---|
| `https://example.com/` from online workstation | live HEAD | **audit failed** (`successful_public_connections=1`) | `outbound-audit-live-host-20260813.json` |
| `https://203.0.113.1/` (TEST-NET) | live HEAD timeout | audit passed (0 successful public) | `outbound-audit-live-probe-blocked-20260813.json` |
| Fixture `-NoNetwork` | not re-used as authority | — | prior 20260812 fixtures only |

## Honest verdict

1. Non-fixture probe path works and correctly **fails** when the host can reach the public internet.
2. This workstation is **not** an air-gapped host; therefore a green outbound audit on `example.com` is **not** expected and was not claimed.
3. TEST-NET timeout “pass” proves the auditor counts failed connections, **not** that a full airgap deploy is isolated.
4. Docker Hub pull for `neo4j` / `curl` failed today (`127.0.0.1:7897` proxy refused), so network-none container drill could not be re-imaged.

## Still open for TRUE airgap sign-off

- Run outbound audit from a truly isolated airgap host/profile where public probes fail.
- Re-run `scripts/offline-frozen-suite.ps1` against current packaged images when registry/proxy is healthy.

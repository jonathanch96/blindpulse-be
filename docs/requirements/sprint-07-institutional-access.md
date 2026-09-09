# Sprint 07 — Institutional access and hardening (backend)

**Status:** PLANNED · **Estimate:** 8–10 dev-days
**Requirements:** FR-AUTH-05..08, NFR-01, NFR-05
**PRD:** §3.6, §6
**Depends on:** Sprint 03 (for the load target), Sprint 06

## Goal

Open the product to prop desks and academies, and prove the NFRs under real load rather than
asserting them.

## Tasks

### 07.1 TradingView SSO (FR-AUTH-05)
- OAuth against TradingView, landing on the existing `sso_provider` / `sso_subject` columns —
  provisioned in Sprint 01's migration precisely so this needs no schema change.
- The same `issueSession` path as password and Google sign-in, so every provider yields an
  identical access/refresh pair and rotation behaviour. A provider with its own session semantics
  is a second auth system to maintain and to get wrong.

### 07.2 Enterprise SAML 2.0 / Okta (FR-AUTH-06)
- SP-initiated flow, IdP metadata per tenant, signature and audience validation, clock-skew
  tolerance, replay protection on assertion IDs.
- Just-in-time provisioning; optional domain allow-list per tenant.
- `SSO_ASSERTION_INVALID` for every verification failure — an error that distinguishes *why* an
  assertion failed is an oracle for forging one.

### 07.3 GitHub and Apple OAuth (FR-AUTH-07)
Same adapter shape as Google; no new session semantics.

### 07.4 Sandbox mode (FR-AUTH-08, currently DEFERRED)
Unauthenticated trial replays. Blocked on an abuse story: an unauthenticated endpoint that creates
sessions and consumes feed capacity needs per-IP rate limiting, a capped session length, and a
disposable account with no persistence. Recorded here so it is a decision, not an oversight.

### 07.5 Distributed rate limiting
Replace the in-process token bucket with the Redis windowed counter already in `pkg/cache`. The
in-process limiter divides the real limit by the replica count, which is silently wrong the moment
the service scales.

### 07.6 NFR verification
- Load test (NFR-01, NFR-02 support): 200 concurrent sessions at 10x, reporting frame latency
  p50/p95/p99, drop rate and CPU headroom. Numbers recorded in the PR, not just a pass/fail.
- Full leak sweep (NFR-05) across every endpoint, including error paths and headers.
- OpenTelemetry traces spanning API → Redis → Kafka → projector, so a slow frame can be attributed.

### 07.7 Operational hardening
- Alert on outbox depth — the signal that matters. A growing `pending` count means projections are
  stale while the API still looks perfectly healthy.
- Alerts on projector lag, websocket disconnect rate, and gate-rejection rate anomalies.
- Runbooks: projection rebuild, relay backlog drain, feed rebuild.

## Acceptance criteria

| # | Given | When | Then |
|---|---|---|---|
| 07-AC-1 | A TradingView account | SSO sign-in | A session identical in shape to a password sign-in |
| 07-AC-2 | An Okta tenant | SAML sign-in | User provisioned JIT; assertion validated for signature, audience and expiry |
| 07-AC-3 | A replayed SAML assertion | Submitted twice | Second attempt refused |
| 07-AC-4 | Any SSO failure mode | Triggered | `SSO_ASSERTION_INVALID` — indistinguishable between causes |
| 07-AC-5 | Three API replicas | Rate limit exercised | The configured limit holds across the cluster, not per replica |
| 07-AC-6 | 200 sessions at 10x | Load test | p99 frame latency < 15ms; numbers in the PR |
| 07-AC-7 | Every endpoint including error paths | Leak sweep | No symbol, no absolute date, no instrument id pre-reveal |

## Definition of done
All criteria met, load-test numbers recorded, alerts wired, and runbooks committed.

# Review — Sprint 01: Identity, accounts and reset trees

**Reviewed:** `services/blindpulse/v1/domain/{user,account}`, `pkg/{hash,jwt,middleware}`,
`migrations/000001`–`000004`, and the frontend's auth and account features.
**Against:** `docs/requirements/sprint-01-identity-accounts.md`, register rows FR-AUTH-01..04,
FR-ACCT-01..05, BR-06/07/11, NFR-07.

## Verdict

**The distinctive part is genuinely well built.** The reset tree and the hash-chained ledger are
the two things this sprint existed to prove, and both do what the document claims — including the
part most projects skip, which is proving that tampering is *detected* rather than merely that the
happy path works.

The identity layer around them is competent but has **one unguarded front door**: the rate limiter
was written, tested, and never wired to a route.

| | ID | Finding |
|---|---|---|
| **High** | `BE-01-1` | `/auth/login` and `/auth/register` have no rate limiting; the limiter exists but is unreachable |
| Medium | `BE-01-2` | `cache.Incr` — the distributed limiter primitive — is dead code |
| Low | `BE-01-3` | `FR-AUTH-04` is PARTIAL and has been since Sprint 01; no account settings screen |
| Low | `BE-01-4` | Controller packages have no tests; auth is exercised only through the domain |

## What holds up

**The ledger chain (NFR-07).** `hashEntry` joins fields with `\x1f` so no boundary can be shifted by
crafting a value that contains a separator — the classic length-extension-by-concatenation mistake,
avoided deliberately. `/accounts/{id}/ledger/verify` recomputes every link and names the *first*
disagreeing sequence rather than returning a bare boolean, which is the difference between a
verification endpoint and a reassurance endpoint.

More to the point, the sprint found a real bug here and the write-up records it honestly: `jsonb`
re-serializes on write and `timestamptz` truncates nanoseconds, so every sealed ledger failed
verification against real PostgreSQL while passing in memory. The fix (a `json` column and
microsecond truncation) is correct, and `TestLedgerEntriesAreWrittenAtDatabaseResolution` now pins
it. **This is the single most valuable thing in the sprint**: an integrity claim that was false in
production and true in tests is worse than no claim, and it was caught.

**Refresh-token rotation.** `Refresh` (`domain/user/service.go:146`) revokes on use, and reuse of an
already-revoked token revokes the entire family for that user and logs it. That is the correct
response to a stolen refresh token and it is more than most implementations do.

**Timing-safe unknown emails.** A dummy Argon2id hash is verified when the email is unknown
(`service.go:18,77`), so a missing account and a wrong password cost the same time and return the
same error. Enumeration is closed properly rather than by hoping nobody measures.

**Database-enforced invariants.** `accounts_single_active_iteration` as a partial unique index, and
the iteration/parent check constraint, mean the reset tree's shape is guaranteed by PostgreSQL
rather than by the application remembering to check. Application-level checks exist too, but only
to turn a constraint violation into an error that names the actual problem.

---

## Findings

### `BE-01-1` · High · Authentication endpoints are unthrottled

**Where:** `services/blindpulse/v1/controllers/auth/controller.go:13-20`, and
`pkg/middleware/rate_limit.go` (the unused limiter).

`RegisterRoutes` mounts `/auth/register`, `/auth/login`, `/auth/google`, `/auth/refresh` and
`/auth/logout` with no middleware. `pkg/middleware/rate_limit.go` implements a token bucket, has
its own passing test, and **is referenced by no non-test file in the repository.**

**Why it matters.** Login is unlimited, so credential stuffing is limited only by the attacker's
bandwidth — and Argon2id makes each attempt expensive *for us*, so an unthrottled login endpoint is
also the cheapest denial-of-service in the codebase. Register is unlimited, so the accounts table is
writable by anyone at any rate.

**Fix.** Wire the existing limiter onto the auth group, keyed by client IP for register and by
IP+email for login. It is a two-line composition-root change; the component is already built and
tested. Then delete the temptation to leave it: assert in a test that the auth group carries the
middleware, because "we wrote a limiter" and "requests are limited" turned out to be different
claims.

### `BE-01-2` · Medium · The distributed rate-limit primitive is dead code

**Where:** `pkg/cache/redis.go:186` (`Incr`), documented as "the primitive behind the distributed
rate limiter".

No caller. The in-memory limiter it was meant to replace is per-process, so on more than one API
replica the effective limit is *N* × the configured one. That is fine while there is one instance
and wrong the moment there are two — and Sprint 03E deliberately built the streaming path to run on
several.

**Fix.** Either use `Incr` behind the limiter's interface when Redis is enabled (matching the
degrade-gracefully pattern the frame bus already uses), or delete it and say in the config comment
that limiting is per-process. Both are defensible; the current state — a primitive that exists and
does nothing — is the one that misleads.

### `BE-01-3` · Low · FR-AUTH-04 has been PARTIAL for the whole project

**Where:** register row FR-AUTH-04; frontend has `PATCH /users/me` and `/users/me/password` routes
and no screen that calls them.

The API and BFF routes exist and work; there is nowhere in the product to change a password. The
register records this honestly (it was downgraded from DONE when it was noticed), so this is
scheduling, not misreporting. It is called out here because it is now the **oldest outstanding
item in the project** and is small enough to have been done four times over.

### `BE-01-4` · Low · No controller-level tests

**Where:** `go test ./...` reports no test files for every package under
`services/blindpulse/v1/controllers/`.

The domain is well covered and the controllers are thin, which is the argument for not testing them.
But the controllers are where request binding, error mapping and — per `BE-01-1` — middleware
composition live, and none of that is exercised except by hand. The auth controller in particular
maps domain errors to HTTP status codes, and nothing checks that a wrong password produces 401
rather than 500.

**Fix.** A handful of `httptest` cases over the auth group would cover binding, error mapping and
the missing rate limiter in one pass.

---

## Documentation drift

- `docs/requirements/sprint-01-identity-accounts.md` read **Status: DONE** with no qualifier while
  the register carried FR-AUTH-04 as PARTIAL. Corrected during this review to match the register's
  "DONE except the account settings screen". A sweep found the same class of drift in sprints 02 and
  03, both of which were delivered and still labelled PLANNED — see `sprint-02-market-data.md`
  finding `BE-02-5`.

## Not checked

Password reset by email (not in scope for Sprint 01 and not implemented), Google ID-token
verification against a live Google (the network policy blocks it — the code path is exercised only
against a stub), and any load or penetration testing.

# Sprint 01 — Identity, accounts and reset trees (backend)

**Status:** DONE · **Reviewed:** [docs/reviews/sprint-01-identity-accounts.md](../reviews/sprint-01-identity-accounts.md)
**Requirements:** FR-AUTH-01..04, FR-ACCT-01..04, BR-06, BR-07, BR-11, NFR-07, NFR-08
**PRD:** §3.5, §3.6, §6.3

## Goal

Deliver the product's second-most distinctive feature — the non-destructive reset tree — and the
identity layer everything else hangs off. The reset tree comes before the replay engine on purpose:
a session must belong to an account, and an account's risk policy is what the order gate will later
enforce.

## Scope delivered

### 01.1 Identity (FR-AUTH-01..04)
- `POST /auth/register`, `/auth/login`, `/auth/google`, `/auth/refresh`, `/auth/logout`.
- `GET|PATCH /users/me`, `PATCH /users/me/password`.
- Argon2id password hashing; a dummy hash is verified on unknown emails so a missing account and a
  wrong password take the same time and return the same error.
- Rotating refresh tokens stored as hashes with an auditable replacement chain; HS256 access tokens
  with a 15-minute default TTL.
- Google ID-token verification against the configured audience. An unset `GOOGLE_CLIENT_ID`
  disables the path rather than half-enabling it.

### 01.2 Accounts as reset trees (FR-ACCT-01..03, BR-06, BR-11)
- `POST /accounts` — opens iteration 1, which is its own `root_account_id`, giving the tree a
  stable identity from the first row.
- `POST /accounts/{id}/reset` — in one transaction: writes a terminal `seal` ledger entry, freezes
  the iteration's root hash, flips it to `reset`, and inserts the child pointing back at it.
  Risk policy and strategy profile are inherited so a reset re-runs the same experiment rather than
  quietly starting a different one.
- `GET /accounts` (trees), `GET /accounts/{id}`, `GET /accounts/{id}/tree`.
- Database-enforced invariants: `accounts_single_active_iteration` (partial unique index),
  `accounts_tree_iteration_key`, and a check that iteration > 1 has a parent while iteration 1
  does not.

### 01.3 The immutable ledger (FR-ACCT-04, BR-07, NFR-07)
- `account_ledger_entries` is append-only. The adapter exposes `Append`, `ListByAccountID` and
  `Last` — there is no update or delete method to call.
- Chain: `entry_hash = SHA256(previous_hash ‖ account ‖ sequence ‖ kind ‖ amount ‖ balance ‖
  equity ‖ payload ‖ recorded_at)`, joined with `\x1f` so no field boundary can be shifted.
- The seal entry's hash becomes the iteration's published root hash: one string attests the whole
  history.
- `GET /accounts/{id}/ledger` and `GET /accounts/{id}/ledger/verify`, the latter recomputing every
  link and naming the first sequence that disagrees.

### 01.4 Events
`account.opened` and `account.reset` written to the outbox in the same transaction as the state
change, keyed by root account id so a tree's events stay ordered on one partition.

## Bug found and fixed during this sprint

The first end-to-end run against real PostgreSQL showed **every sealed ledger failing
verification** although nothing had been tampered with. Two causes, both persistence-layer:

1. `payload` was `jsonb`, which re-serializes on write — it reordered keys and added whitespace,
   changing the exact bytes the hash covers.
2. `recorded_at` was written with Go's nanoseconds; `timestamptz` stores microseconds and rounded
   them away.

Fixed by moving the column to `json` (ledger payloads are read wholesale as an audit record and
never queried by key, so jsonb's indexing bought nothing) and truncating timestamps to microseconds
at write. A regression test asserts both properties.

**Lesson carried into later sprints:** in-memory fakes cannot catch round-trip fidelity bugs. Any
table whose bytes the domain depends on needs a testcontainers-backed adapter test — scheduled in
Sprint 02.

## Acceptance criteria — all met

| # | Criterion | Verified by |
|---|---|---|
| 01-AC-1 | Register → login → refresh → logout round-trips; a rotated refresh token cannot be reused | Domain tests + live smoke |
| 01-AC-2 | Opening an account creates iteration 1 as its own root with an `open` ledger entry | `TestOpenStartsItsOwnTreeWithAnOpeningLedgerEntry` |
| 01-AC-3 | A reset seals the old iteration, keeps it readable, and forks a child inheriting risk and profile | `TestResetSealsTheOldIterationAndKeepsIt` + live |
| 01-AC-4 | Resetting an already-sealed iteration is refused with `ACCOUNT_ARCHIVED` | `TestResetRefusesASealedIteration` |
| 01-AC-5 | Another user's account id reports not-found, never forbidden — existence is itself information | `TestAccountsAreScopedToTheirOwner` |
| 01-AC-6 | An untouched chain verifies and its recomputed root equals the sealed root hash | Live against PostgreSQL |
| 01-AC-7 | A row altered directly in the database reports `valid: false` and names the sequence | Live: `UPDATE ... SET balance_after` → broken at sequence 1 |
| 01-AC-8 | The database refuses a second live iteration in one tree | Live: unique-violation on `accounts_single_active_iteration` |

## Cross-repo contract (what the frontend consumed)

| Endpoint | Shape |
|---|---|
| `POST /accounts` | `{name, strategy_profile?, currency?, initial_balance, risk:{...}}` → `Account` |
| `POST /accounts/{id}/reset` | `{reason, name?, strategy_profile?, initial_balance?, risk?}` → new `Account` |
| `GET /accounts` | `Tree[]` — `{root_account_id, active_account_id, iterations[]}` |
| `GET /accounts/{id}/ledger/verify` | `{account_id, entries, root_hash, stored_root_hash, valid, broken_at_sequence}` |

All decimals are **strings** on the wire (NFR-08). Money never crosses the boundary as a JSON
number, because a JSON number is a float on the other side.

# Sprint 00 — Foundation and infrastructure (backend)

**Status:** DONE · **Requirements:** NFR-06, NFR-08 · **PRD:** §6

## Goal

Stand up the service skeleton and the two pieces of infrastructure every later sprint depends on,
so that no sprint after this one has to argue about where logic lives or how events are delivered.

## Scope delivered

### 00.1 Service skeleton
- `adapters/rest` composition root: config load, gin engine, middleware chain, route registration.
- Middleware: request ID (ULID), structured logging with trace correlation, panic recovery, CORS.
- `pkg/apperror` catalog — one code maps to exactly one HTTP status and one message, and a test
  fails the build if any code referenced in the codebase is missing from the catalog.
- `pkg/response` envelope: `{success, code, message, data, meta, errors, trace_id, timestamp}`.
- `/healthz` (liveness, touches nothing) and `/readyz` (checks Postgres and Redis).
  These are deliberately different: a liveness probe that fails on a slow database gets the
  container killed for somebody else's outage.
- Swagger generation, `cmd/migrate`, `cmd/healthcheck`.

### 00.2 Architecture enforcement
- `tools/archlint` fails CI on: a controller importing a `db/` package, a domain package importing
  gin or gorm, or a GORM model escaping its own package. These are the three ways this layering
  rots in practice, so they are checked mechanically rather than in review.

### 00.3 Redis — the hot path (`pkg/cache`)
- Client with namespaced keys, JSON get/set, `Reserve` (SETNX lease) for idempotency,
  `Incr` for windowed rate limiting, and pub/sub for cross-instance fan-out.
- Everything stored is a cache or a lease; every read has a PostgreSQL fallback.
- An empty `REDIS_ADDR` disables it and the service runs single-instance. Configured-but-
  unreachable fails readiness, because degrading silently behind a load balancer that assumes
  shared state is worse than refusing traffic.

### 00.4 Kafka — the durable spine (`pkg/events/kafka`, `cmd/worker`)
- Publisher with `RequireAll` acks and hash partitioning; consumer with explicit offset commit
  after the handler succeeds, bounded retries, and a logged skip rather than a wedged partition.
- **Transactional outbox** is the only write path (NFR-06). Domain writes append to
  `blindpulse.outbox_events` in the same transaction as their state change; `cmd/worker` drains it.
- Relay claims rows `FOR UPDATE SKIP LOCKED` so replicas drain concurrently without double
  publishing, with exponential backoff and a terminal `failed` state after `KAFKA_MAX_ATTEMPTS`.

### 00.5 Local and deployed environments
- `docker-compose.yml`: Postgres, adminer, Redis, Redpanda (Kafka API), and the relay under an
  `events` profile. `docker-compose.prod.yml`: migrate → api + worker on the shared network.
- CI: lint, archlint, test, build, and a migration run against a clean database.

## Acceptance criteria — all met

| # | Criterion | Verified by |
|---|---|---|
| 00-AC-1 | `make run` serves `/healthz`, `/readyz` and `/api/v1/ping` | Manual + CI |
| 00-AC-2 | `go run ./tools/archlint` passes and fails on a seeded violation | `tools/archlint` own tests |
| 00-AC-3 | Every `apperror` code referenced in code exists in the catalog | `pkg/apperror` test |
| 00-AC-4 | Migrations apply and roll back cleanly on an empty database | CI migrate job; verified locally down-all/up |
| 00-AC-5 | With no broker configured, domain writes still record events and the relay idles | Verified: outbox rows sit `pending` |
| 00-AC-6 | Readiness fails when Redis is configured but unreachable | `handlers.Ready` |

## Definition of done
Build, vet, tests, archlint and the migration job all green in CI; compose brings the full stack up
from a clean checkout with `cp .env.example .env && make up && make migrate-up && make run`.

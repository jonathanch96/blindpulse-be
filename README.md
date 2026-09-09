# BlindPulse Replay Lab — backend

Go 1.26.5 API for BlindPulse Replay Lab: zero-hindsight blinded market replay, server-authoritative
execution simulation, and non-destructive account reset trees.

The service follows the same architecture as `tripmate-be`: Gin, GORM/PostgreSQL, versioned SQL
migrations, Argon2id hashing, rotating refresh tokens, HS256 access tokens, a uniform response
envelope, and strict `controller → domain → database` boundaries enforced by `tools/archlint`.
On top of that it adds the two pieces of infrastructure the replay engine needs: **Redis** for the
hot path and **Kafka** for the durable event spine.

## Local setup

```bash
cp .env.example .env
make up            # postgres + adminer + redis
make migrate-up
make run
curl http://localhost:8080/healthz
curl http://localhost:8080/api/v1/ping
open http://localhost:8080/swagger/index.html
```

`make up-events` brings up the full stack including Redpanda (Kafka API) and the outbox relay.
Run `make test`, `make lint-arch`, and `make build` before opening a PR.

## Architecture

```
adapters/rest/          composition root: config, server, routes, swagger
cmd/migrate             migration runner (production entrypoint)
cmd/worker              outbox relay + event projectors
cmd/healthcheck         container healthcheck probe
pkg/                    shared, domain-free infrastructure
  apperror              error catalog: one code -> one status -> one message
  cache                 Redis client (replay state, idempotency, rate limits, pub/sub)
  events/kafka          publisher + consumer
  jwt, hash, middleware, response, logger, validator, identity
services/blindpulse/v1/
  controllers/          HTTP handlers; no business logic, no SQL
  domain/               business rules; no gin, no gorm
  db/blindpulse/        one package per table: entities, mapper, interface, adapter
  entities/             domain / request / response / event types
  events/emitters       outbox relay
  events/subscribers    projectors (analytics, discipline index)
migrations/             authoritative schema, versioned up/down
```

`tools/archlint` fails the build if a controller imports a `db/` package, if a domain package
imports gin or gorm, or if a GORM model leaks outside its own package.

## Why Redis and Kafka are both here

They solve different problems and neither substitutes for the other.

**Redis is the hot path.** A replay session steps a cursor over a bar series many times a second
and must answer "where am I, what is open, what is my equity" without a database round trip per
tick. It holds session cursor state, decoded bar windows, order idempotency keys, distributed rate
limits, and the pub/sub channel that fans a replay frame to every websocket for a session across
API replicas. Everything in Redis is a cache or a lease: losing it costs speed, never a trade.

**Kafka is the durable spine.** Trades, resets, and reveals are facts other systems need — the
analytics projector that builds the discipline index, the reveal builder, exports. Nothing
publishes to Kafka from a request handler. Domain writes append to `blindpulse.outbox_events` in
the *same transaction* as the state change, and `cmd/worker` drains that table to the broker. That
is what makes an event exactly as durable as the trade that produced it: a broker outage delays
projections and loses nothing, and a rolled-back transaction publishes nothing.

Both degrade rather than crash when unconfigured: an empty `REDIS_ADDR` runs single-instance with
in-process state, and empty `KAFKA_BROKERS` leaves events queued in the outbox.

## Status

Implemented: configuration, server, health/readiness, the full schema, authentication
(register / login / Google / refresh / logout / profile), and accounts with reset trees and a
hash-chained immutable ledger.

Planned, in order: market-data ingestion and blinded feeds, the replay session engine and its
websocket, the order gate and execution simulator, journal and drawings, the mystery reveal, and
the analytics projectors. `PLAN.md` has the sprint-by-sprint breakdown.

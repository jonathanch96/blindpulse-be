.PHONY: run worker build test test-int lint lint-arch swagger migrate-up migrate-down migrate-create mocks up up-events down seed

ifneq (,$(wildcard .env))
include .env
export
endif

MIGRATE_DSN ?= postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_NAME)?sslmode=$(DB_SSLMODE)&search_path=public

run: swagger
	go run ./adapters/rest

worker:
	go run ./cmd/worker

build: swagger
	go build -o bin/blindpulse-api ./adapters/rest
	go build -o bin/blindpulse-worker ./cmd/worker

test: swagger
	go test ./...

test-int: swagger
	go test -tags=integration ./...

lint: lint-arch
	golangci-lint run

lint-arch: swagger
	go run ./tools/archlint

swagger:
	go run github.com/swaggo/swag/cmd/swag@v1.16.6 init -g adapters/rest/main.go -o docs

migrate-up:
	go run -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.1 -path "$(CURDIR)/migrations" -database "$(MIGRATE_DSN)" up

migrate-down:
	go run -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.1 -path "$(CURDIR)/migrations" -database "$(MIGRATE_DSN)" down 1

migrate-create:
	@test -n "$(name)" || (echo "usage: make migrate-create name=<name>" && exit 1)
	go run github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.1 create -ext sql -dir migrations -seq $(name)

mocks:
	go run github.com/vektra/mockery/v2@v2.53.5

up:
	docker compose up -d postgres adminer redis

# The full local stack, including the broker and the outbox relay.
up-events:
	docker compose --profile events up -d

down:
	docker compose --profile events down

seed:
	@echo "No seed data yet. Market bars and blinded feeds are ingested by the loader in Sprint 02."

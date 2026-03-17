SHELL := /bin/bash

GO_CACHE := $(CURDIR)/.cache/go-build
TEST_COMPOSE_FILE := docker-compose.test.yml
TEST_COMPOSE_FILE_ABS := $(CURDIR)/docker-compose.test.yml
TEST_DATABASE_DSN ?= postgres://mdm_test:mdm_test@localhost:55432/mdm_test?sslmode=disable

.PHONY: test
test:
	cd core && GOCACHE="$(GO_CACHE)" go test ./...

.PHONY: test-integration-up
test-integration-up:
	docker compose -f "$(TEST_COMPOSE_FILE)" up -d --wait

.PHONY: test-integration-down
test-integration-down:
	docker compose -f "$(TEST_COMPOSE_FILE)" down -v --remove-orphans

.PHONY: test-integration
test-integration:
	@set -euo pipefail; \
		trap 'docker compose -f "$(TEST_COMPOSE_FILE_ABS)" down -v --remove-orphans' EXIT; \
		docker compose -f "$(TEST_COMPOSE_FILE)" up -d --wait; \
		cd core && TEST_DATABASE_DSN="$(TEST_DATABASE_DSN)" GOCACHE="$(GO_CACHE)" go test -count=1 ./...

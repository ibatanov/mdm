SHELL := /bin/bash

GO_CACHE := $(CURDIR)/.cache/go-build
TEST_COMPOSE_FILE := docker-compose.test.yml
TEST_COMPOSE_FILE_ABS := $(CURDIR)/docker-compose.test.yml
TEST_DATABASE_DSN ?= postgres://mdm_test:mdm_test@localhost:55432/mdm_test?sslmode=disable
UI_DIR := $(CURDIR)/ui
CORE_DIR := $(CURDIR)/core
CACHE_DIR := $(CURDIR)/.cache
BACKEND_PID := $(CACHE_DIR)/mdm-backend.pid
UI_PID := $(CACHE_DIR)/mdm-ui.pid
BACKEND_LOG := $(CACHE_DIR)/mdm-backend.log
UI_LOG := $(CACHE_DIR)/mdm-ui.log

.PHONY: up
up:
	@set -euo pipefail; \
		mkdir -p "$(CACHE_DIR)"; \
		docker compose up -d --wait; \
		if [ -f "$(BACKEND_PID)" ]; then \
			if kill -0 "$$(cat "$(BACKEND_PID)")" 2>/dev/null; then \
				kill "$$(cat "$(BACKEND_PID)")" 2>/dev/null || true; \
				sleep 1; \
			fi; \
			rm -f "$(BACKEND_PID)"; \
		fi; \
		nohup env GOCACHE="$(GO_CACHE)" sh -c 'cd "$(CORE_DIR)" && go run ./cmd/mdm' >"$(BACKEND_LOG)" 2>&1 & \
		echo $$! >"$(BACKEND_PID)"; \
		echo "backend started (pid=$$(cat "$(BACKEND_PID)"))"; \
		if [ -f "$(UI_PID)" ]; then \
			if kill -0 "$$(cat "$(UI_PID)")" 2>/dev/null; then \
				kill "$$(cat "$(UI_PID)")" 2>/dev/null || true; \
				sleep 1; \
			fi; \
			rm -f "$(UI_PID)"; \
		fi; \
		cd "$(UI_DIR)"; \
		if [ ! -d node_modules ]; then npm install; fi; \
		nohup npm run dev -- --host 0.0.0.0 --port 5173 >"$(UI_LOG)" 2>&1 & \
		echo $$! >"$(UI_PID)"; \
		echo "ui started (pid=$$(cat "$(UI_PID)"))"; \
		sleep 1; \
		echo "ui & backend restarted"; \
		echo "MDM contour is up:"; \
		echo "  API:      http://localhost:8080"; \
		echo "  UI:       http://localhost:5173"; \
		echo "  Kafka UI: http://localhost:8088"

.PHONY: restart
restart:
	@set -euo pipefail; \
		$(MAKE) up

.PHONY: down
down:
	@set -euo pipefail; \
		if [ -f "$(UI_PID)" ]; then \
			if kill -0 "$$(cat "$(UI_PID)")" 2>/dev/null; then \
				kill "$$(cat "$(UI_PID)")" 2>/dev/null || true; \
			fi; \
			rm -f "$(UI_PID)"; \
		fi; \
		if [ -f "$(BACKEND_PID)" ]; then \
			if kill -0 "$$(cat "$(BACKEND_PID)")" 2>/dev/null; then \
				kill "$$(cat "$(BACKEND_PID)")" 2>/dev/null || true; \
			fi; \
			rm -f "$(BACKEND_PID)"; \
		fi; \
		docker compose down --remove-orphans

.PHONY: reset
reset:
	@set -euo pipefail; \
		$(MAKE) down; \
		docker compose down -v --remove-orphans

.PHONY: status
status:
	@set -euo pipefail; \
		docker compose ps; \
		if [ -f "$(BACKEND_PID)" ] && kill -0 "$$(cat "$(BACKEND_PID)")" 2>/dev/null; then \
			echo "backend: running (pid=$$(cat "$(BACKEND_PID)"))"; \
		else \
			echo "backend: stopped"; \
		fi; \
		if [ -f "$(UI_PID)" ] && kill -0 "$$(cat "$(UI_PID)")" 2>/dev/null; then \
			echo "ui: running (pid=$$(cat "$(UI_PID)"))"; \
		else \
			echo "ui: stopped"; \
		fi

.PHONY: logs
logs:
	@set -euo pipefail; \
		echo "=== backend log: $(BACKEND_LOG) ==="; \
		tail -n 100 "$(BACKEND_LOG)" 2>/dev/null || true; \
		echo ""; \
		echo "=== ui log: $(UI_LOG) ==="; \
		tail -n 100 "$(UI_LOG)" 2>/dev/null || true

.PHONY: test
test:
	cd "$(CORE_DIR)" && GOCACHE="$(GO_CACHE)" go test ./...

.PHONY: test-integration
test-integration:
	@set -euo pipefail; \
		trap 'docker compose -f "$(TEST_COMPOSE_FILE_ABS)" down -v --remove-orphans' EXIT; \
		docker compose -f "$(TEST_COMPOSE_FILE)" up -d --wait; \
		cd "$(CORE_DIR)" && TEST_DATABASE_DSN="$(TEST_DATABASE_DSN)" GOCACHE="$(GO_CACHE)" go test -count=1 ./...

export GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
export BUILT_AT ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

COMPOSE = docker compose -f deploy/compose.yaml
COMPOSE_FAKE = $(COMPOSE) -f deploy/compose.e2e.yaml

.PHONY: up up-fake down clean logs gen check-gen test test-integration e2e verify-telemetry verify

up:
	$(COMPOSE) up --build --wait

up-fake:
	$(COMPOSE_FAKE) up --build --wait

down:
	-$(COMPOSE_FAKE) down
	$(COMPOSE) down

clean:
	-$(COMPOSE_FAKE) down -v
	$(COMPOSE) down -v

logs:
	$(COMPOSE) logs -f

gen:
	go generate ./...
	@if [ -f web/package.json ]; then npm --prefix web run gen; fi

check-gen: gen
	git diff --exit-code -- api internal web/src/gen web/src/api

test:
	go test ./... -short
	@if [ -f web/package.json ]; then npm --prefix web test -- --run; fi

test-integration:
	go test -tags integration ./...

e2e:
	@if [ -f web/package.json ]; then npm --prefix web exec -- playwright test; fi

verify-telemetry:
	scripts/verify-telemetry.sh all

verify: test-integration test up-fake e2e verify-telemetry

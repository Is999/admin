APP := admin-cron
BIN := bin/$(APP)
VERSION ?= dev
PACKAGE := dist/$(APP)-$(VERSION).tar.gz
LDFLAGS ?= -s -w -X main.buildVersion=$(VERSION)
DOCKER_COMPOSE ?= docker compose
INTEGRATION_MYSQL_DSN ?= root:password@tcp(127.0.0.1:3310)/admin_cron?charset=utf8mb4&parseTime=true&loc=Local
SECRET_SCAN_PATHS := $(wildcard etc/*.sample.yaml) deploy .gitlab-ci.yml Makefile README.md docs/site docs/prometheus docs/grafana
PROMTOOL_IMAGE ?= prom/prometheus:v2.55.1
PROMETHEUS_RULES := $(wildcard docs/prometheus/*.yml)
PROMETHEUS_RULES_IN_CONTAINER := $(patsubst docs/prometheus/%,/rules/%,$(PROMETHEUS_RULES))

.PHONY: fmt fmt-check test build build-tools package check ci diff-check secret-scan promtool-check govulncheck security-scan integration-env-up integration-env-down integration-test migrate-status migrate-dry-run migrate-up clean

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

fmt-check:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))"

test:
	go test ./...

build:
	mkdir -p bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) .

build-tools:
	mkdir -p bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(APP)-migrate ./cmd/migrate

package: build build-tools
	mkdir -p dist
	tar -czf $(PACKAGE) bin etc/*.sample.yaml etc/config.d deploy docs/prometheus docs/grafana docs/site/角色文档/运维 README.md

secret-scan:
	@! grep -R -n -E 'BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY|(^|[[:space:]])rsa_private_key_server:[[:space:]]*[^[:space:]#]+|(^|[[:space:]])aes_key:[[:space:]]*[^[:space:]#]+|(^|[[:space:]])aes_iv:[[:space:]]*[^[:space:]#]+' $(SECRET_SCAN_PATHS)

promtool-check:
	@if command -v promtool >/dev/null 2>&1; then promtool check rules $(PROMETHEUS_RULES); elif command -v docker >/dev/null 2>&1; then docker run --rm --entrypoint promtool -v "$$(pwd)/docs/prometheus:/rules:ro" $(PROMTOOL_IMAGE) check rules $(PROMETHEUS_RULES_IN_CONTAINER); else echo "promtool and docker not found, skip"; fi

govulncheck:
	@if command -v govulncheck >/dev/null 2>&1; then govulncheck ./...; else echo "govulncheck not found, skip"; fi

security-scan: secret-scan govulncheck

diff-check:
	git diff --check

check: fmt-check test build build-tools secret-scan promtool-check diff-check

ci: check

integration-env-up:
	$(DOCKER_COMPOSE) -f deploy/integration/docker-compose.yml up -d

integration-env-down:
	$(DOCKER_COMPOSE) -f deploy/integration/docker-compose.yml down -v

integration-test: integration-env-up
	INTEGRATION_MYSQL_DSN='$(INTEGRATION_MYSQL_DSN)' go test -count=1 -tags=integration ./internal/database

MIGRATE_CONFIG ?= ./etc/config.yaml

migrate-status:
	go run ./cmd/migrate -f $(MIGRATE_CONFIG) -action=status

migrate-dry-run:
	go run ./cmd/migrate -f $(MIGRATE_CONFIG) -action=dry-run

migrate-up:
	go run ./cmd/migrate -f $(MIGRATE_CONFIG) -action=up

clean:
	rm -rf bin dist

.PHONY: dev dev-build build build-backend build-cli build-cz migrate seed lint test test-db test-integration clean setup-tailwind setup-templ setup-air build-css generate-templ generate-code-themes download-mermaid download-htmx docker-build docker-run docker-down

BINARY := dist/cloudzilla
DEV_BINARY := tmp/cloudzilla
GO := /usr/local/go/bin/go
GO_SRC := $(shell find . -name '*.go' -not -path './vendor/*')
TAILWIND := bin/tailwindcss
TAILWIND_OUT := cmd/server/frontend/static/main.css
TEMPL := ~/go/bin/templ
AIR := ~/go/bin/air
DEV_PROXY_PORT := 7331

TAILWIND_VERSION := v4.3.3
HTMX_VERSION := 4.0.0

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GO_LDFLAGS = -X main.version=$(VERSION)

setup-tailwind:                    ## Download Tailwind standalone CLI
	@mkdir -p bin
	@if [ "$$(uname)" = "Darwin" ]; then \
		if [ "$$(uname -m)" = "arm64" ]; then \
			curl -sLf https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-macos-arm64 \
			  -o $(TAILWIND); \
		else \
			curl -sLf https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-macos-x64 \
			  -o $(TAILWIND); \
		fi \
	elif [ "$$(uname)" = "Linux" ]; then \
		if [ "$$(uname -m)" = "aarch64" ]; then \
			curl -sLf https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-linux-arm64 \
			  -o $(TAILWIND); \
		else \
			curl -sLf https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-linux-x64 \
			  -o $(TAILWIND); \
		fi \
	fi \
	&& chmod +x $(TAILWIND)

setup-templ:                       ## Install the templ CLI
	go install github.com/a-h/templ/cmd/templ@latest

setup-air:                         ## Install the air reloader used by `make dev`
	go install github.com/air-verse/air@latest

generate-templ:                    ## Generate *_templ.go from *.templ files
	$(TEMPL) generate ./internal/view/...

generate-code-themes:              ## Regenerate static/code-themes.css from internal/highlight
	$(GO) run ./cmd/gen-code-themes

download-mermaid:                  ## Download mermaid.min.js for self-hosting (one-time)
	@mkdir -p cmd/server/frontend/static
	@if [ ! -f cmd/server/frontend/static/mermaid.min.js ]; then \
		echo "Downloading mermaid.min.js..."; \
		curl -sL https://cdn.jsdelivr.net/npm/mermaid@12/dist/mermaid.min.js \
		  -o cmd/server/frontend/static/mermaid.min.js; \
	fi

download-htmx:                     ## Download htmx.min.js for self-hosting (once per HTMX_VERSION)
	@mkdir -p cmd/server/frontend
	@if ! grep -qsE 'version[:=]"$(HTMX_VERSION)"' cmd/server/frontend/htmx.min.js; then \
		echo "Downloading htmx.min.js $(HTMX_VERSION)..."; \
		curl -sLf https://unpkg.com/htmx.org@$(HTMX_VERSION)/dist/htmx.min.js \
		  -o cmd/server/frontend/htmx.min.js; \
	fi

build-css:                         ## Compile Tailwind → static/main.css
	@mkdir -p cmd/server/frontend/static
	$(TAILWIND) -i tailwind/input.css -o $(TAILWIND_OUT) --minify

# Build only. Regenerating here would rewrite the _templ.go files air watches,
# so every build would trigger the next one; templ's own watcher owns generation.
dev-build:                         ## Build the dev server (air's build step)
	CGO_ENABLED=0 $(GO) build -tags dev -o $(DEV_BINARY) ./cmd/server/.

# templ's watch pattern is narrowed to .templ because its proxy cannot be run
# with the watcher silenced: the proxy only starts once a generation pass has
# happened. Left on its default pattern it also watches .go, and would reload
# the browser the moment a handler changed — before air had rebuilt.
dev: download-mermaid download-htmx build-css generate-templ  ## Rebuild, restart and refresh the browser on save
	@echo "Browse http://localhost:$(DEV_PROXY_PORT) — the app stays on :8080, and so does git."
	@(trap 'kill 0' SIGINT; \
		$(AIR) & \
		$(TAILWIND) -i tailwind/input.css -o $(TAILWIND_OUT) --watch=always & \
		$(TEMPL) generate --watch --watch-pattern '(.+\.templ$$)' --proxy=http://localhost:8080 \
			--proxyport $(DEV_PROXY_PORT) ./internal/view/... & \
		./scripts/dev-notify-proxy.sh & \
		wait)

build: download-mermaid download-htmx build-css generate-templ build-backend build-cli build-cz  ## Full build (server, cz-admin, cz)

build-backend:
	@mkdir -p dist
	$(GO) build -ldflags "$(GO_LDFLAGS)" -o $(BINARY) ./cmd/server/.

build-cli:
	@mkdir -p dist
	$(GO) build -ldflags "$(GO_LDFLAGS)" -o dist/cz-admin ./cmd/cz-admin/.

build-cz:                          ## Build the remote client (needs no CSS or templ step)
	@mkdir -p dist
	CGO_ENABLED=0 $(GO) build -ldflags "$(GO_LDFLAGS)" -o dist/cz ./cmd/cz/.

migrate:
	$(GO) run ./cmd/cz-admin/. migrate

seed:                              ## Fill a fresh, migrated database with test data
	$(GO) run ./cmd/cz-admin/. seed

lint:
	golangci-lint run ./...

test:                              ## Run unit tests (no database required)
	$(GO) test ./...

test-db:                           ## Start the test database container
	docker compose -f docker-compose.test.yml up -d --wait
	@echo "TEST_DATABASE_DSN=postgres://cloudzilla:test@localhost:5433/cloudzilla_test?sslmode=disable"

test-integration: test-db          ## Run all tests including integration tests (requires Docker)
	TEST_DATABASE_DSN=postgres://cloudzilla:test@localhost:5433/cloudzilla_test?sslmode=disable \
	  $(GO) test ./...

clean:
	rm -rf dist/ tmp/
	rm -f $(TAILWIND_OUT) cmd/server/frontend/static/mermaid.min.js

docker-build:              ## Build Docker image
	docker build --build-arg VERSION=$(VERSION) -t cloudzilla-app:latest .

docker-run:                ## Start with docker compose (detached)
	docker compose up -d

docker-down:               ## Stop and remove containers
	docker compose down

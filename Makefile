.PHONY: dev build migrate seed lint test test-db test-integration clean setup-tailwind setup-templ build-css generate-templ generate-code-themes download-mermaid download-htmx docker-build docker-run docker-down

BINARY := dist/cloudzilla
GO := /usr/local/go/bin/go
GO_SRC := $(shell find . -name '*.go' -not -path './vendor/*')
TAILWIND := bin/tailwindcss
TAILWIND_OUT := cmd/server/frontend/static/main.css

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

generate-templ:                    ## Generate *_templ.go from *.templ files
	~/go/bin/templ generate ./internal/view/...

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

dev: download-mermaid download-htmx build-css generate-templ  ## Run backend + Tailwind + templ watch
	@(trap 'kill 0' SIGINT; \
		$(GO) run ./cmd/server/. & \
		$(TAILWIND) -i tailwind/input.css -o $(TAILWIND_OUT) --watch=always & \
		~/go/bin/templ generate --watch ./internal/view/... & \
		wait)

build: download-mermaid download-htmx build-css generate-templ build-backend build-cli  ## Full build (Go + CLI)

build-backend:
	@mkdir -p dist
	$(GO) build -ldflags "$(GO_LDFLAGS)" -o $(BINARY) ./cmd/server/.

build-cli:
	@mkdir -p dist
	$(GO) build -ldflags "$(GO_LDFLAGS)" -o dist/cz-admin ./cmd/cz-admin/.

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
	rm -rf dist/
	rm -f $(TAILWIND_OUT) cmd/server/frontend/static/mermaid.min.js

docker-build:              ## Build Docker image
	docker build --build-arg VERSION=$(VERSION) -t cloudzilla-app:latest .

docker-run:                ## Start with docker compose (detached)
	docker compose up -d

docker-down:               ## Stop and remove containers
	docker compose down

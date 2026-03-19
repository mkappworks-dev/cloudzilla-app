.PHONY: dev build migrate lint test clean setup-tailwind build-css download-mermaid docker-build docker-run docker-down

BINARY := dist/cloudzilla
GO := /usr/local/go/bin/go
GO_SRC := $(shell find . -name '*.go' -not -path './vendor/*')
TAILWIND := bin/tailwindcss
TAILWIND_OUT := cmd/server/frontend/static/main.css

setup-tailwind:                    ## Download Tailwind standalone CLI
	@mkdir -p bin
	@if [ "$$(uname)" = "Darwin" ]; then \
		if [ "$$(uname -m)" = "arm64" ]; then \
			curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-macos-arm64; \
			chmod +x tailwindcss-macos-arm64 && mv tailwindcss-macos-arm64 $(TAILWIND); \
		else \
			curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-macos-x64; \
			chmod +x tailwindcss-macos-x64 && mv tailwindcss-macos-x64 $(TAILWIND); \
		fi \
	elif [ "$$(uname)" = "Linux" ]; then \
		if [ "$$(uname -m)" = "aarch64" ]; then \
			curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-linux-arm64; \
			chmod +x tailwindcss-linux-arm64 && mv tailwindcss-linux-arm64 $(TAILWIND); \
		else \
			curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-linux-x64; \
			chmod +x tailwindcss-linux-x64 && mv tailwindcss-linux-x64 $(TAILWIND); \
		fi \
	fi

download-mermaid:                  ## Download mermaid.min.js for self-hosting (one-time)
	@mkdir -p cmd/server/frontend/static
	@if [ ! -f cmd/server/frontend/static/mermaid.min.js ]; then \
		echo "Downloading mermaid.min.js..."; \
		curl -sL https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js \
		  -o cmd/server/frontend/static/mermaid.min.js; \
	fi

build-css:                         ## Compile Tailwind → static/main.css
	@mkdir -p cmd/server/frontend/static
	$(TAILWIND) -c tailwind/tailwind.config.js -i tailwind/input.css \
	  -o $(TAILWIND_OUT) --minify

dev: download-mermaid build-css    ## Run backend + Tailwind watch
	@(trap 'kill 0' SIGINT; \
		$(GO) run ./cmd/server/. & \
		$(TAILWIND) -c tailwind/tailwind.config.js -i tailwind/input.css \
		  -o $(TAILWIND_OUT) --watch & \
		wait)

build: download-mermaid build-css build-backend build-cli  ## Full build (Go + CLI)

build-backend:
	@mkdir -p dist
	$(GO) build -o $(BINARY) ./cmd/server/.

build-cli:
	@mkdir -p dist
	$(GO) build -o dist/cloudzilla-cli ./cmd/cloudzilla/.

migrate:
	$(GO) run ./cmd/cloudzilla/. migrate

lint:
	golangci-lint run ./...

test:
	$(GO) test ./...

clean:
	rm -rf dist/
	rm -f $(TAILWIND_OUT) cmd/server/frontend/static/mermaid.min.js cloudzilla.db cloudzilla.db-shm cloudzilla.db-wal

docker-build:              ## Build Docker image
	docker build -t cloudzilla:latest .

docker-run:                ## Start with docker compose (detached)
	docker compose up -d

docker-down:               ## Stop and remove containers
	docker compose down

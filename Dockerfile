# ── Stage 1: Build ──────────────────────────────────────────────────────────
FROM golang:1.27-alpine AS builder

# libstdc++ and libgcc: the Tailwind CLI's musl build links them dynamically
RUN apk add --no-cache make curl libstdc++ libgcc

WORKDIR /app
COPY . .

# Download Tailwind CLI (arch-aware, pinned version; musl builds for Alpine) and build CSS
RUN ARCH=$(uname -m) && \
    if [ "$ARCH" = "aarch64" ]; then TW=tailwindcss-linux-arm64-musl; else TW=tailwindcss-linux-x64-musl; fi && \
    mkdir -p bin cmd/server/frontend/static && \
    curl -sLf "https://github.com/tailwindlabs/tailwindcss/releases/download/v4.3.3/${TW}" \
      -o bin/tailwindcss && \
    chmod +x bin/tailwindcss && \
    bin/tailwindcss -i tailwind/input.css -o cmd/server/frontend/static/main.css --minify

# Download mermaid.min.js and htmx.min.js for embedding
RUN curl -sL https://cdn.jsdelivr.net/npm/mermaid@12/dist/mermaid.min.js \
      -o cmd/server/frontend/static/mermaid.min.js && \
    curl -sL https://unpkg.com/htmx.org@4.0.0/dist/htmx.min.js \
      -o cmd/server/frontend/htmx.min.js

# Build Go binaries — fully static, stripped
# Declared here, not at the top: a new VERSION re-runs every RUN after the ARG.
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=${VERSION}" -o dist/cloudzilla ./cmd/server/. && \
    CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=${VERSION}" -o dist/cloudzilla-cli ./cmd/cloudzilla/.

# ── Stage 2: Runtime ─────────────────────────────────────────────────────────
FROM alpine:3.24

# ca-certificates: needed for Google OAuth outbound HTTPS
# tzdata: correct timestamps in commits/issues/PRs
# postgresql18-client: pg_dump and pg_restore for `cloudzilla-cli backup` and `restore`; its major must be at least the server's (compose runs postgres:18)
RUN apk add --no-cache ca-certificates tzdata postgresql18-client

WORKDIR /app
COPY --from=builder /app/dist/cloudzilla     /app/cloudzilla
COPY --from=builder /app/dist/cloudzilla-cli /app/cloudzilla-cli

# Data directory for git repos, uploaded files, SSH host key
RUN mkdir -p /data/git-repos /data/storage
ENV CZ_STORAGE_LOCAL_ROOT=/data/storage

EXPOSE 8080 2222

# Liveness, not readiness: a restart fixes neither a database outage nor a pending migration.
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -q -Y off -O /dev/null "http://127.0.0.1:${CZ_SERVER_PORT:-8080}/healthz" || exit 1

ENTRYPOINT ["/app/cloudzilla"]

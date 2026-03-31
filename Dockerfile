# ── Stage 1: Build ──────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache make curl

WORKDIR /app
COPY . .

# Download Tailwind CLI (arch-aware, pinned version) and build CSS
RUN ARCH=$(uname -m) && \
    if [ "$ARCH" = "aarch64" ]; then TW=tailwindcss-linux-arm64; else TW=tailwindcss-linux-x64; fi && \
    mkdir -p bin cmd/server/frontend/static && \
    curl -sLf "https://github.com/tailwindlabs/tailwindcss/releases/download/v3.4.19/${TW}" \
      -o bin/tailwindcss && \
    chmod +x bin/tailwindcss && \
    bin/tailwindcss -c tailwind/tailwind.config.js -i tailwind/input.css \
      -o cmd/server/frontend/static/main.css --minify

# Download mermaid.min.js for embedding
RUN curl -sL https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js \
      -o cmd/server/frontend/static/mermaid.min.js

# Build Go binaries — fully static, stripped
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o dist/cloudzilla ./cmd/server/. && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o dist/cloudzilla-cli ./cmd/cloudzilla/.

# ── Stage 2: Runtime ─────────────────────────────────────────────────────────
FROM alpine:3.21

# ca-certificates: needed for Google OAuth outbound HTTPS
# tzdata: correct timestamps in commits/issues/PRs
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/dist/cloudzilla     /app/cloudzilla
COPY --from=builder /app/dist/cloudzilla-cli /app/cloudzilla-cli

# Data directory for SQLite DB, git repos, SSH host key
RUN mkdir -p /data/git-repos

EXPOSE 8080 2222

ENTRYPOINT ["/app/cloudzilla"]

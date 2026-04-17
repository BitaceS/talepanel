# ── Stage 1: Build ────────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

# Install CA certificates and git (some modules need git at build time).
RUN apk add --no-cache ca-certificates git

WORKDIR /build

# Download dependencies.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source and build both binaries.
COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
      -ldflags="-s -w" \
      -trimpath \
      -o /build/talepanel-api \
      ./cmd/server

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
      -ldflags="-s -w" \
      -trimpath \
      -o /build/tale-cli \
      ./cmd/talecli

# ── Stage 2: Runtime (production) ─────────────────────────────────────────────
FROM alpine:3.19 AS production

RUN apk add --no-cache ca-certificates wget

# Copy both compiled binaries.
COPY --from=builder /build/talepanel-api /usr/local/bin/talepanel-api
COPY --from=builder /build/tale-cli /usr/local/bin/tale-cli

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/talepanel-api"]

# ── Stage 3: Development ──────────────────────────────────────────────────────
# Used by docker-compose for local development (go run, live code via volume).
FROM golang:1.25-alpine AS development

RUN apk add --no-cache ca-certificates git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

EXPOSE 8080

ENV CGO_ENABLED=0

CMD ["go", "run", "-mod=mod", "./cmd/server"]

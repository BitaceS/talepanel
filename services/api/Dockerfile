# ── Stage 1: Build ────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

# Install CA certificates and git (some modules need git at build time).
RUN apk add --no-cache ca-certificates git

WORKDIR /build

# Download dependencies. go.sum is generated here if not present.
COPY go.mod ./
RUN go mod download

# Copy the rest of the source and build the binary.
COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
      -ldflags="-s -w" \
      -trimpath \
      -o /build/talepanel-api \
      ./cmd/server

# ── Stage 2: Runtime (production) ─────────────────────────────────────────────
FROM alpine:3.19 AS production

RUN apk add --no-cache ca-certificates wget

# Copy the compiled binary.
COPY --from=builder /build/talepanel-api /talepanel-api

EXPOSE 8080

ENTRYPOINT ["/talepanel-api"]

# ── Stage 3: Development ──────────────────────────────────────────────────────
# Used by docker-compose for local development (go run, live code via volume).
FROM golang:1.22-alpine AS development

RUN apk add --no-cache ca-certificates git

WORKDIR /app

COPY go.mod ./
# Download and tidy generates go.sum if it doesn't exist yet
RUN go mod download && go mod tidy

COPY . .

EXPOSE 8080

ENV CGO_ENABLED=0

CMD ["go", "run", "-mod=mod", "./cmd/server"]

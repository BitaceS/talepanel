# ─────────────────────────────────────────────────────────────────────────────
# Stage 1: Builder
#
# Uses the official Rust Debian image (glibc) so proc-macro crates like
# async-trait can be compiled as dylibs.  musl/Alpine images do not support
# proc-macro dylibs.
# ─────────────────────────────────────────────────────────────────────────────
FROM rust:1.98-slim-bookworm AS builder

# Install build tools and OpenSSL headers (reqwest rustls-tls still pulls in
# openssl-sys on some platforms).
RUN apt-get update && apt-get install -y --no-install-recommends \
        pkg-config \
        libssl-dev \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*

ENV CARGO_NET_RETRY=5

WORKDIR /build

# ── Dependency caching layer ────────────────────────────────────────────────
COPY Cargo.toml Cargo.lock ./

RUN mkdir -p src && \
    echo 'fn main() {}' > src/main.rs && \
    mkdir -p src/process && \
    touch src/config.rs src/api_client.rs src/heartbeat.rs \
          src/command_poller.rs src/http_server.rs src/net_monitor.rs \
          src/process/mod.rs src/process/mock.rs src/process/manager.rs

RUN cargo build --release 2>&1 | tail -5

# ── Real source build ────────────────────────────────────────────────────────
RUN rm -rf src/

COPY src/ src/

RUN touch src/main.rs && \
    cargo build --release

# ─────────────────────────────────────────────────────────────────────────────
# Stage 2: Runtime image
#
# Slim Debian image — matches the builder's glibc so the dynamic binary runs.
# ─────────────────────────────────────────────────────────────────────────────
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        tzdata \
        curl \
        unzip \
        wget \
        gnupg \
        nftables \
        conntrack \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /etc/taledaemon /srv/taledaemon/servers /srv/taledaemon/downloads /srv/taledaemon/tools \
    && chmod -R 777 /srv/taledaemon

# Install Eclipse Temurin JRE 25 (current Hytale server builds target Java 25 /
# class file version 69.0; Java 21 only reaches 65.0 and fails to load them).
RUN mkdir -p /etc/apt/keyrings \
    && wget -qO - https://packages.adoptium.net/artifactory/api/gpg/key/public | gpg --dearmor -o /etc/apt/keyrings/adoptium.gpg \
    && echo "deb [signed-by=/etc/apt/keyrings/adoptium.gpg] https://packages.adoptium.net/artifactory/deb bookworm main" > /etc/apt/sources.list.d/adoptium.list \
    && apt-get update && apt-get install -y --no-install-recommends temurin-25-jre \
    && rm -rf /var/lib/apt/lists/*

# rclone — transport for off-site backups (OneDrive, Google Drive, Dropbox,
# S3, B2, SFTP, WebDAV).  Pinned release from downloads.rclone.org, verified
# against the SHA-256 published in that release's SHA256SUMS.  Debian's own
# package (1.60 in bookworm) is too old for current OneDrive/Drive APIs.
ARG RCLONE_VERSION=1.75.2
ARG RCLONE_SHA256_AMD64=349ac8fba6ff65d6247043f1750cdcb518ec5d500ef91463a10d37c0ccdf3702
ARG RCLONE_SHA256_ARM64=7e1e8d69654941b7b7df84ee74c5f7fb09cce7d5496947fb7bf16b55ff427d10
RUN set -eu; \
    arch="$(dpkg --print-architecture)"; \
    case "$arch" in \
        amd64) sum="$RCLONE_SHA256_AMD64" ;; \
        arm64) sum="$RCLONE_SHA256_ARM64" ;; \
        *) echo "no pinned rclone checksum for $arch" >&2; exit 1 ;; \
    esac; \
    tmp="$(mktemp -d)"; \
    curl -fsSL -o "$tmp/rclone.zip" "https://downloads.rclone.org/v${RCLONE_VERSION}/rclone-v${RCLONE_VERSION}-linux-${arch}.zip"; \
    echo "$sum  $tmp/rclone.zip" | sha256sum -c -; \
    unzip -q "$tmp/rclone.zip" -d "$tmp"; \
    install -m 0755 "$tmp/rclone-v${RCLONE_VERSION}-linux-${arch}/rclone" /usr/local/bin/rclone; \
    rm -rf "$tmp"; \
    rclone version | head -n 1

COPY --from=builder /build/target/release/taledaemon /usr/local/bin/taledaemon

EXPOSE 8444

ENTRYPOINT ["/usr/local/bin/taledaemon"]

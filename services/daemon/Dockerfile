# ─────────────────────────────────────────────────────────────────────────────
# Stage 1: Builder
#
# Uses the official Rust Debian image (glibc) so proc-macro crates like
# async-trait can be compiled as dylibs.  musl/Alpine images do not support
# proc-macro dylibs.
# ─────────────────────────────────────────────────────────────────────────────
FROM rust:1.85-slim-bookworm AS builder

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

# Install Eclipse Temurin JRE 21 (Hytale requires Java 21 / class file version 65.0)
RUN mkdir -p /etc/apt/keyrings \
    && wget -qO - https://packages.adoptium.net/artifactory/api/gpg/key/public | gpg --dearmor -o /etc/apt/keyrings/adoptium.gpg \
    && echo "deb [signed-by=/etc/apt/keyrings/adoptium.gpg] https://packages.adoptium.net/artifactory/deb bookworm main" > /etc/apt/sources.list.d/adoptium.list \
    && apt-get update && apt-get install -y --no-install-recommends temurin-21-jre \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /build/target/release/taledaemon /usr/local/bin/taledaemon

EXPOSE 8444

ENTRYPOINT ["/usr/local/bin/taledaemon"]

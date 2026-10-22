# syntax=docker/dockerfile:1.7

# Build the weed binary from a pinned SeaweedFS release. We build from source
# instead of pulling the official image because the official image is Alpine
# based, and rpie's Raspberry Pi vendor kernel returns garbage from the 32-bit
# ARM clock_gettime64 syscall, which musl 1.2 relies on for time; that breaks
# TLS inside Alpine containers on rpie, so apk add and any other TLS traffic
# fail there. Debian (glibc) uses the legacy clock and works. Each host builds
# its own native architecture, so docker compose up -d --build works on rpie
# (armv7) and amd64 alike without a registry.
#
# The 32-bit ARM alignment fix we used to carry as seaweedfs-arm32-align.patch
# (weed/cluster.LiveLock.generation landed on a 4-byte address, so the atomic
# 64-bit fencing token panicked with "unaligned 64-bit atomic operation") is
# upstream since 4.45: https://github.com/seaweedfs/seaweedfs/pull/11028
# Do not pin below 4.45 on armv7.
FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS build
ARG SEAWEEDFS_VERSION=4.48
ARG TARGETOS TARGETARCH TARGETVARIANT

RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
RUN curl -fsSL "https://github.com/seaweedfs/seaweedfs/archive/refs/tags/${SEAWEEDFS_VERSION}.tar.gz" \
    | tar -xz --strip-components=1 -C /src

RUN cd /src/weed && \
    export GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" && \
    case "${TARGETARCH}" in arm) export GOARM="${TARGETVARIANT#v}";; esac && \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -extldflags -static" -o /out/weed .

FROM --platform=$TARGETPLATFORM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/weed /usr/bin/weed
COPY entrypoint.sh /usr/local/bin/seaweedfs-entrypoint
RUN chmod +x /usr/local/bin/seaweedfs-entrypoint && mkdir -p /data /etc/seaweedfs
VOLUME /data
EXPOSE 8333
ENTRYPOINT ["/usr/local/bin/seaweedfs-entrypoint"]

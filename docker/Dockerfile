# syntax=docker/dockerfile:1.7

# Build the weed binary from SeaweedFS source with the 32-bit ARM alignment
# fix applied (seaweedfs-arm32-align.patch). The official chrislusf/seaweedfs
# image panics on armv7 ("unaligned 64-bit atomic operation" in
# weed/cluster.LiveLock.generation), so we build our own with the one-line fix.
#
# We use Debian (glibc) base images, not Alpine. rpie's Raspberry Pi vendor
# kernel returns garbage from the 32-bit ARM clock_gettime64 syscall, which
# musl 1.2 relies on for time; that breaks TLS inside Alpine containers on
# rpie, so apk add / git clone fail there. glibc uses the legacy clock and
# works. Each host builds its own native architecture, so docker compose
# up -d --build works on rpie (armv7) and amd64 alike without a registry.
FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS build
ARG SEAWEEDFS_VERSION=4.42
ARG TARGETOS TARGETARCH TARGETVARIANT

RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
RUN git clone --depth 1 --branch "${SEAWEEDFS_VERSION}" https://github.com/seaweedfs/seaweedfs .

COPY seaweedfs-arm32-align.patch /tmp/seaweedfs-arm32-align.patch
RUN git apply --whitespace=nowarn /tmp/seaweedfs-arm32-align.patch

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

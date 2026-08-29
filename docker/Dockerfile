# syntax=docker/dockerfile:1.7

# Build the weed binary from SeaweedFS source with the 32-bit ARM alignment
# fix applied (seaweedfs-arm32-align.patch). The official chrislusf/seaweedfs
# image panics on armv7 ("unaligned 64-bit atomic operation" in
# weed/cluster.LiveLock.generation), so we build our own with the one-line fix.
# The same image builds natively on every host: each architecture compiles its
# own binary, so docker compose up -d --build works on rpie (armv7) and amd64
# alike without a registry.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
ARG SEAWEEDFS_VERSION=4.42
ARG TARGETOS TARGETARCH TARGETVARIANT

RUN apk add --no-cache git

WORKDIR /src
RUN git clone --depth 1 --branch "${SEAWEEDFS_VERSION}" https://github.com/seaweedfs/seaweedfs .

COPY seaweedfs-arm32-align.patch /tmp/seaweedfs-arm32-align.patch
RUN git apply --whitespace=nowarn /tmp/seaweedfs-arm32-align.patch

RUN cd /src/weed && \
    export GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" && \
    case "${TARGETARCH}" in arm) export GOARM="${TARGETVARIANT#v}";; esac && \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -extldflags -static" -o /out/weed .

FROM --platform=$TARGETPLATFORM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/weed /usr/bin/weed
COPY entrypoint.sh /usr/local/bin/seaweedfs-entrypoint
RUN chmod +x /usr/local/bin/seaweedfs-entrypoint && mkdir -p /data /etc/seaweedfs
VOLUME /data
EXPOSE 8333
ENTRYPOINT ["/usr/local/bin/seaweedfs-entrypoint"]

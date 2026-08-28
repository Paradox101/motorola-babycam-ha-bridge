# syntax=docker/dockerfile:1
#
# Build context MUST be the repository root (the parent Go module lives there):
#
#   docker build -f addon/motorola-magic-bridge/Dockerfile \
#     --build-arg BUILD_FROM=ghcr.io/home-assistant/amd64-base:latest \
#     -t motorola-magic-bridge:local .
#
# Home Assistant passes BUILD_FROM/BUILD_ARCH from build.yaml automatically.

# --- Go build stage ---------------------------------------------------------
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
# No external dependencies: the module is std-lib only, so no `go mod download`.
COPY internal ./internal
COPY cmd ./cmd
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /out/magicbridge ./cmd/magicbridge

# --- Runtime stage ----------------------------------------------------------
ARG BUILD_FROM
FROM ${BUILD_FROM}

ARG BUILD_ARCH=amd64
ARG GO2RTC_VERSION=1.9.4

# Map the Home Assistant arch to the matching go2rtc release asset and install it.
RUN set -eu; \
    case "${BUILD_ARCH}" in \
      amd64)   GO2RTC_ARCH="amd64" ;; \
      aarch64) GO2RTC_ARCH="arm64" ;; \
      armv7)   GO2RTC_ARCH="arm" ;; \
      armhf)   GO2RTC_ARCH="armv6" ;; \
      i386)    GO2RTC_ARCH="i386" ;; \
      *)       echo "unsupported arch ${BUILD_ARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /usr/bin/go2rtc \
      "https://github.com/AlexxIT/go2rtc/releases/download/v${GO2RTC_VERSION}/go2rtc_linux_${GO2RTC_ARCH}"; \
    chmod +x /usr/bin/go2rtc

COPY --from=build /out/magicbridge /usr/bin/magicbridge
COPY addon/motorola-magic-bridge/run.sh /run.sh
RUN chmod +x /run.sh

CMD [ "/run.sh" ]

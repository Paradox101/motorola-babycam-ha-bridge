# Home Assistant add-on image: vm65-bridge + go2rtc.
#
# The Go source is built from the public repo at SOURCE_REF, so the add-on is
# self-contained (no prebuilt release required). The go2rtc binary is copied
# from its official multi-arch image. HA supervisor builds this per target arch
# natively (via qemu), so the default GOARCH already matches the target.
ARG BUILD_ARCH=amd64
ARG GO_VERSION=1.27.0

FROM golang:${GO_VERSION}-alpine AS build
RUN apk add --no-cache git
# Override to build a branch/tag/commit other than the default.
ARG SOURCE_REF=v0.4.0
RUN git clone --depth 1 --branch ${SOURCE_REF} \
    https://github.com/Paradox101/motorola-babycam-ha-bridge /src
WORKDIR /src
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /out/vm65-bridge ./cmd/vm65-bridge && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /out/vm65-setup ./cmd/vm65-setup

FROM alexxit/go2rtc:1.9.14 AS go2rtc

FROM ghcr.io/home-assistant/${BUILD_ARCH}-base:3.19
COPY --from=build /out/vm65-bridge /usr/local/bin/vm65-bridge
COPY --from=build /out/vm65-setup /usr/local/bin/vm65-setup
COPY --from=go2rtc /usr/local/bin/go2rtc /usr/local/bin/go2rtc
COPY run.sh /run.sh
RUN chmod +x /run.sh
CMD ["/run.sh"]

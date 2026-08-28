# Home Assistant add-on image: vm65-bridge + go2rtc.
#
# The Go source is built from the public repo at SOURCE_REF, so the add-on is
# self-contained (no prebuilt release required). The go2rtc binary is copied
# from its official multi-arch image. HA supervisor builds this per target arch
# natively (via qemu), so the default GOARCH already matches the target.
ARG BUILD_FROM
ARG GO_VERSION=1.27

FROM golang:${GO_VERSION}-alpine AS build
RUN apk add --no-cache git
# Override to build a branch/tag/commit other than the default.
ARG SOURCE_REF=main
RUN git clone --depth 1 --branch ${SOURCE_REF} \
    https://github.com/Paradox101/motorola-vm65-bridge /src
WORKDIR /src
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /out/vm65-bridge ./cmd/vm65-bridge

FROM alexxit/go2rtc:latest AS go2rtc

FROM ${BUILD_FROM}
COPY --from=build /out/vm65-bridge /usr/local/bin/vm65-bridge
COPY --from=go2rtc /usr/local/bin/go2rtc /usr/local/bin/go2rtc
COPY run.sh /run.sh
COPY go2rtc.tmpl.yaml /etc/go2rtc.tmpl.yaml
RUN chmod +x /run.sh
CMD ["/run.sh"]

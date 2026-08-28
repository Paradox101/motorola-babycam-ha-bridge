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
COPY config.yaml /tmp/vm65-addon-config.yaml
# The add-on builds from the release branch, not from a version tag. A tag can
# only be created after the commit that names it, so pinning to one left every
# build broken in the window between the release commit and the tag push. The
# release branch always exists and is moved to the released commit, so this
# value never changes and the build never depends on a ref that is not there
# yet. Override to build another branch, tag or the default branch.
ARG SOURCE_REF=release
RUN git clone --depth 1 --branch "${SOURCE_REF}" \
        https://github.com/Paradox101/motorola-babycam-ha-bridge /src || { \
        echo "ERROR: '${SOURCE_REF}' does not exist in the repository."; \
        echo "The add-on builds from the 'release' branch. Point it at the"; \
        echo "released commit and push it:"; \
        echo "    git push origin main:release"; \
        echo "To build the default branch instead, pass --build-arg SOURCE_REF=main."; \
        exit 1; \
    }
WORKDIR /src
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /out/vm65-bridge ./cmd/vm65-bridge && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /out/vm65-setup ./cmd/vm65-setup

FROM alexxit/go2rtc:1.9.14 AS go2rtc

FROM ghcr.io/home-assistant/${BUILD_ARCH}-base:3.19
# go2rtc needs ffmpeg to turn an H264 keyframe into a JPEG. Without it every
# still-image request fails, which reaches Home Assistant as a 500 from its
# image proxy and leaves the snapshot entity permanently broken. The official
# go2rtc image bundles ffmpeg; only the binary is copied from it, so it has to
# be installed here.
RUN apk add --no-cache ffmpeg
COPY --from=build /out/vm65-bridge /usr/local/bin/vm65-bridge
COPY --from=build /out/vm65-setup /usr/local/bin/vm65-setup
COPY --from=go2rtc /usr/local/bin/go2rtc /usr/local/bin/go2rtc
COPY run.sh /run.sh
RUN chmod +x /run.sh
CMD ["/run.sh"]

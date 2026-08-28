# Multi-arch build for the vm65-bridge daemon.
# Produces a static (CGO-disabled) binary, so the runtime image is scratch.
#
#   docker buildx build --platform linux/amd64,linux/arm64 -t vm65-bridge .
#
# BUILDPLATFORM/TARGETARCH are provided by buildx; a plain `docker build`
# defaults TARGETARCH to the host arch.
ARG GO_VERSION=1.27

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
WORKDIR /src

# Module graph first for layer caching. There are no third-party deps, so this
# stays a single go.mod; keep the pattern for when that changes.
COPY go.mod ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/vm65-bridge ./cmd/vm65-bridge

FROM scratch
COPY --from=build /out/vm65-bridge /usr/local/bin/vm65-bridge
# The relay hosts are reached over the network; bring CA roots for any TLS the
# runtime may add later, plus a home for the credentials mount.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
EXPOSE 8554 8555
ENTRYPOINT ["/usr/local/bin/vm65-bridge"]
CMD ["-listen", "0.0.0.0:8554", "-status", "0.0.0.0:8555", "-creds", "/data/creds.json"]

# vm65-bridge build helpers.
#
#   make test          run unit tests (race detector)
#   make build         build the daemon for the host
#   make dist          cross-compile static binaries for amd64 + arm64
#   make docker        buildx a multi-arch image (needs docker buildx)
#   make vet fmt       static checks

GO       ?= go
BIN       := vm65-bridge
PKG       := ./cmd/vm65-bridge
DISTDIR   := dist
IMAGE     ?= vm65-bridge
PLATFORMS ?= linux/amd64,linux/arm64
LDFLAGS   := -s -w

.PHONY: all
all: fmt vet test build

.PHONY: test
test:
	$(GO) test -race ./...

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: fmt
fmt:
	$(GO) fmt ./...

.PHONY: build
build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN) $(PKG)

.PHONY: dist
dist:
	@mkdir -p $(DISTDIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DISTDIR)/$(BIN)-linux-amd64 $(PKG)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DISTDIR)/$(BIN)-linux-arm64 $(PKG)
	@echo "built:" && ls -1 $(DISTDIR)

.PHONY: docker
docker:
	docker buildx build --platform $(PLATFORMS) -t $(IMAGE) .

.PHONY: clean
clean:
	rm -rf $(BIN) $(DISTDIR)

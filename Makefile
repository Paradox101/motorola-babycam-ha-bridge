# vm65-bridge build helpers.
#
#   make test          run unit tests (race detector)
#   make build         build the daemon for the host
#   make dist          cross-compile static binaries for amd64 + arm64
#   make docker        buildx a multi-arch image (needs docker buildx)
#   make check          formatting, vet, tests and repository policy

GO       ?= go
BIN       := vm65-bridge
PKG       := ./cmd/vm65-bridge
DISTDIR   := dist
IMAGE     ?= vm65-bridge
PLATFORMS ?= linux/amd64,linux/arm64
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null)
LDFLAGS   := -s -w -X github.com/local/motorola-vm65-bridge/internal/buildinfo.Version=$(VERSION)

.PHONY: all
all: check build

.PHONY: check
check: fmt-check vet test policy addon-check shell-check

.PHONY: test
test:
	$(GO) test -race ./...

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: fmt
fmt:
	$(GO) fmt ./...

.PHONY: fmt-check
fmt-check:
	@unformatted="$$($$($(GO) env GOROOT)/bin/gofmt -l cmd internal)"; \
	if [ -n "$$unformatted" ]; then \
		echo "These files need gofmt:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

.PHONY: policy
policy:
	pwsh -NoProfile -File tools/ci/check-repository.ps1

.PHONY: addon-check
addon-check:
	python tools/ci/check_addon.py
	python -m unittest discover -s tools/ci -p 'test_*.py' -v

.PHONY: shell-check
shell-check:
	shellcheck homeassistant/vm65-bridge/run.sh
	bats homeassistant/vm65-bridge/tests/run.bats

.PHONY: build
build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN) $(PKG)

.PHONY: dist
dist:
	@mkdir -p $(DISTDIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DISTDIR)/$(BIN)-linux-amd64 $(PKG)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DISTDIR)/vm65-setup-linux-amd64 ./cmd/vm65-setup
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DISTDIR)/$(BIN)-linux-arm64 $(PKG)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DISTDIR)/vm65-setup-linux-arm64 ./cmd/vm65-setup
	@echo "built:" && ls -1 $(DISTDIR)

.PHONY: docker
docker:
	docker buildx build --platform $(PLATFORMS) -t $(IMAGE) .

.PHONY: clean
clean:
	rm -rf $(BIN) $(DISTDIR)

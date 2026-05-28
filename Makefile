BINARY_OC := bin/oc

VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0-dev")
LDFLAGS  := -ldflags "-X main.version=$(VERSION)"

.DEFAULT_GOAL := build

# ── Build ──────────────────────────────────────────────────────────────────────

.PHONY: build
build: $(BINARY_OC)

$(BINARY_OC): $(shell find cmd/oc internal pkg -name '*.go')
	@mkdir -p bin
	go build $(LDFLAGS) -o $@ ./cmd/oc

.PHONY: install
install:
	go install $(LDFLAGS) ./cmd/oc

# ── Dev ────────────────────────────────────────────────────────────────────────

.PHONY: restart
restart: $(BINARY_OC)
	./$(BINARY_OC) daemon restart

.PHONY: run
run: $(BINARY_OC)
	./$(BINARY_OC) daemon --log-level debug

.PHONY: tidy
tidy:
	go mod tidy

# ── Test & Lint ────────────────────────────────────────────────────────────────

.PHONY: test
test:
	go test ./...

.PHONY: test-verbose
test-verbose:
	go test -v ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: lint
lint:
	@which golangci-lint >/dev/null 2>&1 || (echo "golangci-lint not found; install from https://golangci-lint.run/usage/install/" && exit 1)
	golangci-lint run ./...

.PHONY: check
check: vet test

# ── Clean ──────────────────────────────────────────────────────────────────────

.PHONY: clean
clean:
	rm -rf bin/

# ── Release ────────────────────────────────────────────────────────────────────

PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

.PHONY: release
release:
	@mkdir -p dist
	@for platform in $(PLATFORMS); do \
		os=$$(echo $$platform | cut -d/ -f1); \
		arch=$$(echo $$platform | cut -d/ -f2); \
		echo "Building $$os/$$arch..."; \
		ver="$(VERSION)"; case "$$ver" in v*) tag="$$ver";; *) tag="v$$ver";; esac; \
		dist_abs=$$(pwd)/dist; \
		tmpdir=$$(mktemp -d); \
		bin="oc"; if [ "$$os" = "windows" ]; then bin="oc.exe"; fi; \
		GOOS=$$os GOARCH=$$arch go build $(LDFLAGS) -o "$$tmpdir/$$bin" ./cmd/oc; \
		mkdir -p "$$tmpdir/collectors/browser"; \
		cp -R collectors/browser/chrome "$$tmpdir/collectors/browser/chrome"; \
		if [ "$$os" = "windows" ]; then \
			(cd "$$tmpdir" && zip -qr "$$dist_abs/oc-$$tag-$$os-$$arch.zip" "$$bin" collectors); \
		else \
			(cd "$$tmpdir" && tar czf "$$dist_abs/oc-$$tag-$$os-$$arch.tar.gz" "$$bin" collectors); \
		fi; \
		rm -rf "$$tmpdir"; \
	done
	@echo "Release archives written to dist/"

# ── Help ───────────────────────────────────────────────────────────────────────

.PHONY: help
help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "  build         Build oc binary to bin/"
	@echo "  install       Install binaries to GOPATH/bin"
	@echo "  restart       Build and restart installed OpenContext daemon"
	@echo "  tidy          Run go mod tidy"
	@echo "  test          Run all tests"
	@echo "  vet           Run go vet"
	@echo "  lint          Run golangci-lint (must be installed)"
	@echo "  check         Run vet + test"
	@echo "  clean         Remove bin/"
	@echo "  release       Cross-compile release archives"

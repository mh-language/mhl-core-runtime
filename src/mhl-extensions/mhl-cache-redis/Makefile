# mhl-cache-redis — an official Redis-backed `cache` extension for mhl.
GO   ?= go
NAME := mhl-cache-redis
BIN  := bin/$(NAME)
DIST := dist/$(NAME)

.PHONY: build test vet up down smoke dist release clean

build: $(BIN)

$(BIN): $(wildcard *.go) go.mod
	$(GO) build -o $(BIN) .
	@command -v codesign >/dev/null 2>&1 && codesign --force --sign - $(BIN) || true

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

up:
	docker compose up -d --wait

down:
	docker compose down -v

smoke: build up
	./smoke.sh

# Metadata-only package under dist/$(NAME): extension.mh and README.md — what
# `mhl lint`, the LSP and a reviewer read without the binary. `make release`
# adds the executables and tars it.
DIST_OSARCH ?= linux/amd64 linux/arm64 darwin/arm64 darwin/amd64 windows/amd64

dist:
	rm -rf $(DIST)
	mkdir -p $(DIST)
	cp extension.mh README.md $(DIST)/
	@echo "packaged $(NAME) metadata -> $(DIST)"

# Install-ready archive: the dist metadata plus bin/$(NAME)-<goos>-<goarch>
# for every target (CGO off — all deps are pure Go), tarred. This is what
# `mhl extension install` consumes — from dist/release/$(NAME).tar.gz or the
# now-populated dist/$(NAME)/ tree.
release: dist
	@set -e; mkdir -p $(DIST)/bin; for t in $(DIST_OSARCH); do \
	  os=$${t%%/*}; arch=$${t#*/}; sfx=; case $$os in windows) sfx=.exe ;; esac; \
	  echo "  $(NAME): $$os/$$arch"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "-s -w" -o $(DIST)/bin/$(NAME)-$$os-$$arch$$sfx .; \
	done
	mkdir -p dist/release
	tar -C dist -czf dist/release/$(NAME).tar.gz $(NAME)
	@echo "released $(NAME) ($(words $(DIST_OSARCH)) platforms) -> dist/release/$(NAME).tar.gz"

clean:
	rm -rf bin dist

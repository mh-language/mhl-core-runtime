# mhl-store-s3 — an official S3-backed `store` extension for mhl.
GO   ?= go
BIN  := bin/mhl-store-s3

.PHONY: build test vet up down smoke install-example clean

build: $(BIN)

$(BIN): $(wildcard *.go) go.mod
	$(GO) build -o $(BIN) .
	@command -v codesign >/dev/null 2>&1 && codesign --force --sign - $(BIN) || true

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

# Bring up local MinIO and create the bucket (blocks until it exists).
up:
	docker compose up -d minio
	docker compose run --rm createbucket

down:
	docker compose down -v

# End-to-end: MinIO + build + install into a scratch project + run a workflow
# that exercises get/put/delete/list against S3.
smoke: build up
	./smoke.sh

clean:
	rm -rf bin

SHELL := /bin/sh

MODULE := github.com/mh-language/mhl-core-runtime
CMD := ./cmd/mhl
DIST := dist
SAMPLE := ../../sample
VERSION := $(shell git describe --tags --match 'v[0-9]*.[0-9]*.[0-9]*' --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X $(MODULE)/internal/cli.Version=$(VERSION)

.PHONY: build test functional-test release clean verify-release redhat mac-apple-silicon linux-arm64

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/mhl $(CMD)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(SAMPLE)/mhl $(CMD)

test:
	go test ./...

# runs the sample/ tree (docs-facing .mh examples, migrated from the old
# test/e2e/{lang/syntax,features}) through the binary build produces above
functional-test: build
	$(SAMPLE)/mhl test $(SAMPLE)/syntax
	$(SAMPLE)/mhl test $(SAMPLE)/features

# Red Hat runs the standard Linux binaries; build both supported CPU architectures.
redhat: $(DIST)/linux-amd64/mhl $(DIST)/linux-arm64/mhl

# M3, M4, and other Apple Silicon Macs use the same darwin/arm64 binary.
mac-apple-silicon: $(DIST)/darwin-arm64/mhl

release: redhat mac-apple-silicon $(DIST)/windows-amd64/mhl.exe

$(DIST):
	mkdir -p $(DIST)

$(DIST)/linux-amd64/mhl: $(shell find . -name '*.go' -type f)
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o $@ $(CMD)

$(DIST)/darwin-arm64/mhl: $(shell find . -name '*.go' -type f)
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o $@ $(CMD)

$(DIST)/windows-amd64/mhl.exe: $(shell find . -name '*.go' -type f)
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o $@ $(CMD)

# Keep the existing single-architecture target for callers that need it.
linux-arm64: $(DIST)/linux-arm64/mhl
$(DIST)/linux-arm64/mhl: $(shell find . -name '*.go' -type f)
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o $@ $(CMD)

verify-release: release
	test -s $(DIST)/linux-amd64/mhl
	test -s $(DIST)/linux-arm64/mhl
	test -s $(DIST)/darwin-arm64/mhl
	test -s $(DIST)/windows-amd64/mhl.exe
	@echo "release verified: CGO disabled for all targets"

clean:
	rm -rf $(DIST)

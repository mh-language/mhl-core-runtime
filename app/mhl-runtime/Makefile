SHELL := /bin/sh

MODULE := github.com/yanjustino/mhl-runtime
CMD := ./cmd/mhl
DIST := dist
LDFLAGS := -s -w

.PHONY: build test release clean verify-release

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/mhl $(CMD)

test:
	go test ./...

release: $(DIST)/mhl-linux-amd64 $(DIST)/mhl-darwin-arm64 $(DIST)/mhl-windows-amd64.exe

$(DIST):
	mkdir -p $(DIST)

$(DIST)/mhl-linux-amd64: $(shell find . -name '*.go' -type f)
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o $@ $(CMD)

$(DIST)/mhl-darwin-arm64: $(shell find . -name '*.go' -type f)
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o $@ $(CMD)

$(DIST)/mhl-windows-amd64.exe: $(shell find . -name '*.go' -type f)
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o $@ $(CMD)

verify-release: release
	test -s $(DIST)/mhl-linux-amd64
	test -s $(DIST)/mhl-darwin-arm64
	test -s $(DIST)/mhl-windows-amd64.exe
	@echo "release verified: CGO disabled for all targets"

clean:
	rm -rf $(DIST)

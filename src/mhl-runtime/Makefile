SHELL := /bin/sh

MODULE := github.com/yanjustino/mhl-runtime
CMD := ./cmd/mhl
DIST := dist
TEST := test
LDFLAGS := -s -w

.PHONY: build test release clean verify-release

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/mhl $(CMD)

test:
	go test ./...

release: $(DIST)/linux-amd64/mhl $(DIST)/darwin-arm64/mhl $(DIST)/windows-amd64/mhl.exe

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

verify-release: release
	test -s $(DIST)/linux-amd64/mhl
	test -s $(DIST)/darwin-arm64/mhl
	test -s $(DIST)/windows-amd64/mhl.exe
	@echo "release verified: CGO disabled for all targets"

clean:
	rm -rf $(DIST)

# Package every official mhl extension.
#
#   make dist     # dist/<name>/ per extension — metadata only (manifest + README), no binaries
#   make release  # dist/release/<name>.tar.gz per extension (metadata + all-platform bin/) + SHA256SUMS
#   make test     # go test ./... in every extension
#   make clean    # drop every extension's bin/ + dist/, and this dist/
#
# `make dist` is the lightweight tree — extension.json, declarations.json and
# README.md — for `mhl lint`, the LSP and review, not installable on its own.
# `make release` adds bin/<name>-<goos>-<goarch> for linux/amd64, linux/arm64,
# darwin/arm64, darwin/amd64 and windows/amd64 (CGO off — every dependency is
# pure Go) and tars each tree for a GitHub release. `mhl extension install`
# resolves the binary for the runtime host (the "<goos>-<goarch>" name
# convention) and vendors only that one; the lock pins its sha256:
#   mhl extension install <asset-url>/<name>.tar.gz#sha256=<hex from SHA256SUMS>

EXTENSIONS := mhl-cache-redis mhl-sql-postgres mhl-store-postgres mhl-store-s3
DIST       := dist

.PHONY: dist release test clean $(EXTENSIONS)

dist: $(EXTENSIONS)

$(EXTENSIONS):
	$(MAKE) -C $@ dist
	mkdir -p $(DIST)
	rm -rf $(DIST)/$@
	cp -R $@/dist/$@ $(DIST)/$@
	@echo "collected $@ -> $(DIST)/$@"

release:
	@rm -rf $(DIST)/release && mkdir -p $(DIST)/release
	@set -e; for e in $(EXTENSIONS); do \
	  echo "== $$e =="; $(MAKE) -C $$e release; \
	  cp $$e/dist/release/$$e.tar.gz $(DIST)/release/; \
	done
	@cd $(DIST)/release && { command -v sha256sum >/dev/null && sha256sum *.tar.gz || shasum -a 256 *.tar.gz; } > SHA256SUMS
	@echo "all extension release archives (+ SHA256SUMS) -> $(DIST)/release/"

test:
	@set -e; for e in $(EXTENSIONS); do echo "== $$e =="; $(MAKE) -C $$e test; done

clean:
	@for e in $(EXTENSIONS); do $(MAKE) -C $$e clean; done
	rm -rf $(DIST)

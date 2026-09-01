# syntax=docker/dockerfile:1
#
# Container image for `mhl serve mcp --http`, sized for the single-pod
# Kubernetes smoke test in tests/cloud/k8s/ (see tests/cloud/mhl-eks-design.html
# for the target architecture).
#
# Build from the REPO ROOT so the build stage can see src/mhl-runtime/:
#
#   docker build -f tests/cloud/Dockerfile -t mhl-serve:local .
#
# Multi-stage: the builder cross-compiles for the requested TARGETARCH, so the
# same Dockerfile produces a linux/arm64 image on Apple Silicon and linux/amd64
# elsewhere — no host toolchain or Makefile target needed.

# ---- build ----------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS build

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

WORKDIR /src
# module cache layer
COPY src/mhl-runtime/go.mod src/mhl-runtime/go.sum ./
RUN go mod download

# sources + the two workflows this server exposes
COPY src/mhl-runtime/ ./

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath \
      -ldflags "-s -w -X github.com/mh-language/mhl-core-runtime/internal/cli.Version=${VERSION}" \
      -o /out/mhl ./cmd/mhl

# ---- runtime -------------------------------------------------------------
# alpine, not distroless: the sample workflows shell out via
# cmd.exec(["sh","-c",...]) (DocPipeline.Draft, all of SlowBuild), so the image
# MUST carry /bin/sh. Still ~10 MB, still runs as non-root with a read-only
# root fs (writes go to the /state and /tmp mounts).
FROM alpine:3.21

RUN apk add --no-cache ca-certificates \
 && addgroup -g 65532 nonroot \
 && adduser -u 65532 -G nonroot -D -H nonroot \
 && mkdir -p /state /workflows \
 && chown nonroot:nonroot /state

COPY --from=build /out/mhl /usr/local/bin/mhl
COPY tests/cloud/docs-workflow.mh tests/cloud/slow-build.mh /workflows/

USER 65532:65532
EXPOSE 8711

# exec form: mhl is PID 1 and receives SIGTERM straight from the kubelet, so
# the graceful drain (CENARIO-009 / CENARIO-K8S-001) actually fires. Everything
# tunable is an MHL_SERVE_* env var set by the Deployment; only --addr and the
# workflow dir are baked here.
# NB: mhl's flag parser wants space-separated values (no --flag=value form).
ENTRYPOINT ["/usr/local/bin/mhl", "serve", "mcp", "--http", "--addr", "0.0.0.0:8711", "/workflows"]

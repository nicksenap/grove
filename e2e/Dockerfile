# Hermetic environment for the e2e suite.
#
# e2e/run.sh sandboxes itself (its own HOME, GIT_CONFIG_GLOBAL, TMPDIR) and is the
# suite we run on macOS and in CI directly. This image exists for what sandboxing
# cannot cover: a guarantee that nothing on the host is reachable at all, a pinned
# git version, and a Linux filesystem where symlink and permission behavior differ
# from macOS.
#
# It deliberately runs the same script rather than a container-specific variant —
# two suites would drift, and the point is one suite in two environments.
#
#   docker build -f e2e/Dockerfile -t grove-e2e .
#   docker run --rm grove-e2e
#
# Or: just e2e-docker

FROM golang:1.26-bookworm

# git is the software under orchestration; jq drives the assertions.
RUN apt-get update \
    && apt-get install -y --no-install-recommends git jq ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Dependencies first, so source edits do not re-download the module cache.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -ldflags "-X github.com/nicksenap/grove/cmd.Version=e2e-docker" -o /usr/local/bin/gw ./cmd/gw

# Run as a non-root user. root masks permission bugs, because it can write
# anywhere regardless of the modes Grove sets.
RUN useradd --create-home --shell /bin/bash grove \
    && chown -R grove:grove /src
USER grove

ENV GW_BIN=/usr/local/bin/gw

# The one network-dependent test (plugin install over HTTPS) skips itself when
# GitHub is unreachable, so this passes under --network=none too.
CMD ["bash", "e2e/run.sh"]

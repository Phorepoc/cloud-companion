# syntax=docker/dockerfile:1
# SPDX-License-Identifier: Apache-2.0
# The web app (Quasar, MIT) is built first and embedded into the Go binary.
FROM --platform=$BUILDPLATFORM node:22-bookworm-slim@sha256:4b150b48b4116e2a22f2c39cc27025fbf4f153e2078fd3b14d159fdb5c7279da AS web
WORKDIR /web
COPY web/package.json web/package-lock.json web/.npmrc ./
# the Etesian design packages come from GitHub Packages: a read:packages token as a build secret (never kept in the image), e.g.
#   GITHUB_PACKAGES_TOKEN=... docker build --secret id=github_packages_token,env=GITHUB_PACKAGES_TOKEN .
RUN --mount=type=secret,id=github_packages_token,env=GITHUB_PACKAGES_TOKEN npm ci --no-audit --no-fund
COPY web/ ./
# subset the icon font to the icons the app uses (tools/subset-icons.py)
RUN apt-get update && apt-get install -y --no-install-recommends python3-fonttools python3-brotli && rm -rf /var/lib/apt/lists/* && python3 tools/subset-icons.py
RUN npm run build

# built on the build machine for the target (arm64 for Raspberry Pi boards): Go cross-compiles, no emulation
FROM --platform=$BUILDPLATFORM golang:1.26-bookworm@sha256:23ce71337b726608afb1bc00986e630a05034a87a87ea1fc2cd1ae5cf33387eb AS build
ARG TARGETARCH
WORKDIR /src
# goolm: Matrix encryption in pure Go (mautrix-go), so the binary needs no cgo or libolm
ENV GOFLAGS=-tags=goolm CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist internal/webui/dist
RUN GOARCH=$TARGETARCH go build -trimpath -o /out/phecloud ./cmd/phecloud \
 && GOARCH=$TARGETARCH go build -trimpath -o /out/phecloud-client ./cmd/phecloud-client

# the browser suites' stand-ins for outside services (cmd/fakesvc): a test tool, never in the release image (built only by name, the
# last stage below is the default)
FROM build AS fakes
RUN GOARCH=$TARGETARCH go build -trimpath -o /usr/local/bin/fakesvc ./cmd/fakesvc
ENTRYPOINT ["fakesvc"]

# Chromium is an external runtime dependency (not vendored), pinned to one version.
# The full browser (not headless-shell), in a window on a virtual display (Xvfb, started by the entrypoint).
FROM debian:trixie-slim@sha256:a72b4433de1cf7d6bae0c4cee74d0547f58601a643b3375f070c51600d63b6b2
RUN apt-get update && apt-get install -y --no-install-recommends chromium=156.0.8078.79-1~deb13u1 xvfb \
    ca-certificates fonts-liberation fonts-dejavu-core fonts-noto-core \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/ /usr/local/bin/
ENV PHECLOUD_TLSDIR=/data PHECLOUD_DB=/data/phecloud.db PHECLOUD_CHROME=/usr/bin/chromium
VOLUME /data
EXPOSE 7400 8080
ENV DISPLAY=:99
# a restarted container keeps /tmp: the old display's lock goes first, and the browser starts once the display is there
ENTRYPOINT ["/bin/sh", "-c", "rm -f /tmp/.X99-lock /tmp/.X11-unix/X99; Xvfb :99 -screen 0 1920x1080x24 -nolisten tcp >/dev/null 2>&1 & for i in $(seq 100); do [ -e /tmp/.X11-unix/X99 ] && break; sleep 0.1; done; exec /usr/local/bin/phecloud \"$@\"", "phecloud"]
CMD ["serve"]

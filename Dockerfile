# syntax=docker/dockerfile:1
# SPDX-License-Identifier: Apache-2.0
# The web app (Quasar, MIT) is built first and embedded into the Go binary.
FROM node:22-bookworm-slim@sha256:4b150b48b4116e2a22f2c39cc27025fbf4f153e2078fd3b14d159fdb5c7279da AS web
WORKDIR /web
COPY web/package.json web/package-lock.json web/.npmrc ./
# the Etesian design packages come from GitHub Packages: a read:packages token as a build secret (never kept in the image), e.g.
#   GITHUB_PACKAGES_TOKEN=... docker build --secret id=github_packages_token,env=GITHUB_PACKAGES_TOKEN .
RUN --mount=type=secret,id=github_packages_token,env=GITHUB_PACKAGES_TOKEN npm ci --no-audit --no-fund
COPY web/ ./
# subset the icon font to the icons the app uses (tools/subset-icons.py)
RUN apt-get update && apt-get install -y --no-install-recommends python3-fonttools python3-brotli && rm -rf /var/lib/apt/lists/* && python3 tools/subset-icons.py
RUN npm run build

FROM golang:1.26-bookworm@sha256:23ce71337b726608afb1bc00986e630a05034a87a87ea1fc2cd1ae5cf33387eb AS build
WORKDIR /src
# goolm: Matrix encryption in pure Go (mautrix-go), so the binary needs no cgo or libolm
ENV GOFLAGS=-tags=goolm CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist internal/webui/dist
RUN go build -trimpath -o /out/phecloud ./cmd/phecloud \
 && go build -trimpath -o /out/phecloud-client ./cmd/phecloud-client

# the browser suites' stand-ins for outside services (cmd/fakesvc): a test tool, never in the release image (built only by name, the
# last stage below is the default)
FROM build AS fakes
RUN go build -trimpath -o /usr/local/bin/fakesvc ./cmd/fakesvc
ENTRYPOINT ["fakesvc"]

# Chromium is an external runtime dependency (not vendored), pinned to one version.
FROM chromedp/headless-shell:156.0.8078.48@sha256:3da74b0945e4f80f00f0310bcda9f76cee283539ebd200a053a7bebf3de89423
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates fonts-liberation fonts-dejavu-core fonts-noto-core \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/ /usr/local/bin/
ENV PHECLOUD_TLSDIR=/data PHECLOUD_DB=/data/phecloud.db PHECLOUD_CHROME=/headless-shell/headless-shell
VOLUME /data
EXPOSE 7400 8080
ENTRYPOINT ["/usr/local/bin/phecloud"]
CMD ["serve"]

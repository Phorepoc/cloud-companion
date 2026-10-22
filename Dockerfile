# SPDX-License-Identifier: Apache-2.0
# The web app (Quasar, MIT) is built first and embedded into the Go binary.
FROM node:22-bookworm-slim AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npx vite build

FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist internal/webui/dist
RUN CGO_ENABLED=0 go build -trimpath -o /out/phecloud ./cmd/phecloud \
 && CGO_ENABLED=0 go build -trimpath -o /out/phecloud-client ./cmd/phecloud-client

# Chromium is an external runtime dependency (not vendored), pinned to one version.
FROM chromedp/headless-shell:156.0.8078.48
RUN apt-get update && apt-get install -y --no-install-recommends fonts-liberation fonts-dejavu-core fonts-noto-core \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/ /usr/local/bin/
ENV PHECLOUD_TLSDIR=/data PHECLOUD_DB=/data/phecloud.db PHECLOUD_CHROME=/headless-shell/headless-shell
VOLUME /data
EXPOSE 7400 8080
ENTRYPOINT ["/usr/local/bin/phecloud"]
CMD ["serve"]

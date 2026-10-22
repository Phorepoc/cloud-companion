# SPDX-License-Identifier: Apache-2.0
FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/phecloud ./cmd/phecloud \
 && CGO_ENABLED=0 go build -trimpath -o /out/phecloud-client ./cmd/phecloud-client

# Chromium is an external runtime dependency (not vendored), pinned to one version.
FROM chromedp/headless-shell:156.0.8078.48
RUN apt-get update && apt-get install -y --no-install-recommends fonts-liberation fonts-dejavu-core fonts-noto-core \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/ /usr/local/bin/
ENV PHECLOUD_DB=/data/phecloud.db PHECLOUD_CHROME=/headless-shell/headless-shell
VOLUME /data
EXPOSE 7400
ENTRYPOINT ["/usr/local/bin/phecloud"]
CMD ["serve"]

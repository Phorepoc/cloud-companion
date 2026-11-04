# pheandroid: the Android-to-browser bridge. adb (Apache-2.0) comes from Debian; the scrcpy server jar (Apache-2.0) is NOT in the image: mount it at /scrcpy-server.jar.
FROM golang:1.26@sha256:670bb5aa5349ae03b8516764718486f4f16399134bdc9acd0c46a23f043ab05c AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /pheandroid ./cmd/pheandroid
FROM debian:bookworm-slim@sha256:99c0b25c46d0214d5a2091092e7908907dcaecd6a962cde0ac700acee222b6f3
RUN apt-get update && apt-get install -y --no-install-recommends adb ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=build /pheandroid /usr/local/bin/pheandroid
USER nobody
ENV HOME=/tmp
ENTRYPOINT ["/usr/local/bin/pheandroid"]

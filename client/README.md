# client: the portable C core

`phc.[ch]`, `phc_cbor.[ch]`, `phc_sha256.[ch]`: the phecloud phone-protocol client as a C99 library (no malloc, no OS calls;
the transport and image decoding are the host's). `phc_mbedtls.[ch]`: a TLS 1.3 transport on mbedTLS 3.6 (external
dependency, not vendored; the certificate is pinned by SHA-256, the socket is non-blocking). See `SYMBIAN.md` for how it
becomes a web-engine plug-in.

```
make -C client all check fuzz   # build, offline self-test, and the same self-test (with frame fuzzing) under ASan + UBSan
make -C client MBEDTLS=/path/to/mbedtls-3.6 all     # also the TLS adapter and a `host_test -tls`
sh client/test/run.sh           # on a Docker host, from the repo root: everything, against a live server
```
`run.sh` fetches and builds mbedTLS into `.deps/` (first run), builds/tests/fuzzes the core, starts the server, and runs the Go
test client and the C host test (TLS 1.3, pinned, non-blocking) on example.com (tap through the C hit-test, history back),
the layers page (typed text), a Wikipedia article, and a "gaps" page (focus and keyboard, select, JavaScript prompt, refused
download). It compares tile count, tile bytes, XOR of tile pixel hashes, regions, layers, title, history flags, focus, select,
dialog and download fields between the two clients, and checks that both refuse a changed server certificate.
`test/compose.plain.yml` adds a development-only plain-TCP listener (`PHECLOUD_PLAIN_LISTEN`) for a build without TLS.

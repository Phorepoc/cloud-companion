# client: the portable C core

`phc.[ch]`, `phc_cbor.[ch]`, `phc_sha256.[ch]`: the phecloud phone-protocol client as a C99 library (no malloc, no OS calls;
the transport and image decoding are the host's). See `SYMBIAN.md` for how it becomes a web-engine plug-in.

```
make -C client all check        # build + offline self-test (gcc or clang)
sh client/test/run.sh           # on a Docker host, from the repo root: live cross-check against the Go test client
```
`run.sh` starts the server with a development-only plain-TCP listener (`PHECLOUD_PLAIN_LISTEN=:7401`, see
`test/compose.plain.yml`; production is TLS 1.3 only), runs the Go client and the C host test on example.com, the layers page
and a Wikipedia article, and compares tile count, tile bytes, the XOR of all tile pixel hashes, regions and layers; it also
checks that a changed server certificate is refused.

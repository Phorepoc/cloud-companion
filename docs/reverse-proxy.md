# Running phecloud behind a reverse proxy

The web app and its WebSocket live on port **8080, plain HTTP**. Put a proxy in front to get TLS (needed for passkeys and for any use
outside localhost). The phone protocol on **7400** is a different thing, see the end.

## The one setting: `PHECLOUD_TRUSTED_PROXIES`
```
PHECLOUD_TRUSTED_PROXIES=172.18.0.0/16,10.0.0.5     # IPs and CIDRs of the proxy as phecloud sees it (the TCP peer)
```
- **Empty (the default): nobody is trusted.** Forwarding headers are ignored; this is the safe setting for direct access.
- A request whose TCP peer is in the list is believed about its public **scheme**, **host** and **client address**. From any other peer
  the headers are ignored, so a client that reaches port 8080 directly cannot pick its own origin or address.
- Read, in this order: `Forwarded` (`for`, `host`, `proto`), `X-Forwarded-Proto|Host|For`, Cloudflare's `CF-Visitor` and `CF-Connecting-IP`.
  The client address is the first `X-Forwarded-For` entry from the right that is not itself a trusted proxy. For scheme and host the
  **last** value is used (the one the nearest trusted proxy wrote), so the proxy should **set** these headers, not forward the client's.
- Used for: the WebAuthn **relying party id and origin** (the public host name, `https`), the WebSocket origin check, the rate limiter's client
  address and the log. The app sets no cookies and builds no absolute URLs, and the browser chooses `wss` when the page is `https`.
- `PHECLOUD_RP_ID` and `PHECLOUD_RP_ORIGINS` still override what is derived, for odd setups.
- A passkey belongs to a **domain name**: browse to `https://cloud.example.org`, not to an IP address (the app says so when you do).
  Keep the same name for good; a different name means new passkeys.

Check it: `curl -s https://cloud.example.org/passkey/status` answers JSON, and in the browser console
`fetch('/passkey/register/begin',{method:'POST',body:'{}'})` is refused with the admin-code message (not a relying-party error).

## Traefik (tested)
`web/test/compose.traefik.yml` and `web/test/traefik/dynamic.yml` run phecloud behind Traefik v3 with TLS on `websecure`; only Traefik's
address is trusted (`PHECLOUD_TRUSTED_PROXIES=172.31.77.10/32`). `sh web/test/proxy.sh` runs a browser with a virtual authenticator:
the relying party id becomes `phecloud.test`, a passkey registers and signs in over `https`/`wss`, and a client that talks to phecloud directly
with `X-Forwarded-Host: evil.example` / `Forwarded` / `CF-Connecting-IP` still gets its own host.
Key lines (dynamic file):
```yaml
http:
  routers:  { phecloud: { rule: Host(`cloud.example.org`), entryPoints: [websecure], tls: {}, service: phecloud } }
  services: { phecloud: { loadBalancer: { servers: [ { url: "http://phecloud:8080" } ] } } }
```
Traefik sets `X-Forwarded-Proto/Host/For` itself. Trust the network or address Traefik uses to reach phecloud.

### Traefik with Tailscale certificates (a `*.ts.net` name)
Give the machine a tailnet name (`tailscale up`, MagicDNS and HTTPS enabled in the admin console), then either
- let Traefik fetch the certificate: `--certificatesresolvers.ts.tailscale=true` and `tls: { certResolver: ts }` on the router, Traefik must
  reach the host's `tailscaled` socket (`/var/run/tailscale/tailscaled.sock`); or
- make the files once and renew them: `tailscale cert host.tailnet-name.ts.net` and point Traefik at them:
  ```yaml
  tls:
    certificates:
      - certFile: /certs/host.tailnet-name.ts.net.crt
        keyFile:  /certs/host.tailnet-name.ts.net.key
  ```
The router rule is `Host(`host.tailnet-name.ts.net`)`; the passkey relying party id is then that name. Phones and browsers on the tailnet reach
it by that name. (Configuration as above; the test in this repo uses Traefik's default certificate and a local name, it does not run Tailscale.)

## Caddy
```
cloud.example.org {
	reverse_proxy phecloud:8080
}
```
Caddy sets the forwarding headers and keeps the `Host`. Trust Caddy's address. The repository's `deploy/Caddyfile*` and the `tls` compose
profile do this (and the hub variant sends `/auth` to the mail server, which is why phecloud's own endpoints are under `/passkey`).

## nginx
```
location / {
    proxy_pass http://phecloud:8080;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $remote_addr;     # set, do not append, when nginx is the first proxy
    proxy_set_header Upgrade $http_upgrade;            # the WebSocket at /ws
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 1h;
}
```

## Cloudflare Tunnel
`cloudflared` runs next to phecloud and connects out to Cloudflare; no port is opened. In the tunnel's public hostname:
`cloud.example.org` -> `http://phecloud:8080`. Cloudflare terminates TLS and sends `CF-Connecting-IP`, `CF-Visitor`, `X-Forwarded-For` and
`X-Forwarded-Proto`; trust the address `cloudflared` has on the docker network (`PHECLOUD_TRUSTED_PROXIES=<cloudflared ip>/32`). WebSockets
work through the tunnel. (Not run in this repository's tests; the header handling is covered by the unit test for `CF-*`.)

## The phone port (7400) is not HTTP
The phones speak a framed protocol over **raw TCP with TLS 1.3 and a pinned certificate** on port 7400. An HTTP proxy or an HTTP-only tunnel
(Cloudflare Tunnel as above, Traefik's HTTP routers) cannot carry it, and a proxy that terminates TLS would break the pin. Options:
1. **A private network** between the phone and the server (Tailscale/headscale, or the phetail tunnel): the phone connects to the tailnet address
   or name on port 7400; nothing is public. Simplest and what the phone pairing screen expects.
2. **TCP passthrough**: open 7400 on the server, or use a TLS-passthrough TCP router (Traefik `tcp` router with `tls: { passthrough: true }` on
   an SNI, nginx `stream` with `ssl_preread`). The certificate stays phecloud's own, so the pin keeps working.
3. **`cloudflared access tcp`** on a machine that can run it (a PC, not the phone) to reach a Cloudflare-published TCP service.
4. Future: carry the phone protocol over a WebSocket (it already is one for the web app), so that an HTTP tunnel can serve phones too.

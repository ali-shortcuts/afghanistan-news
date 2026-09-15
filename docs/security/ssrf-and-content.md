# Security posture

## SSRF protection (feed fetching)

Feeds are remote URLs supplied by third parties through an OPML file, so the fetcher is the
platform's highest-risk surface. Controls, in order:

1. **Scheme** — only `http` and `https`; everything else is rejected before DNS.
2. **Host blocklist** — `localhost`, `*.local`, `*.internal`, `*.home.arpa`, metadata hostnames.
3. **Literal IP checks** — loopback, RFC1918, link-local (incl. `169.254.169.254`), CGNAT,
   multicast, documentation and reserved ranges are refused.
4. **DNS resolution check** — the resolved addresses are validated before the connection.
5. **Dial-time re-check** — a custom `DialContext` re-validates the peer IP, closing the
   DNS-rebinding window.
6. **Redirect re-check** — every hop repeats 1–5; redirects are capped.
7. **Port policy** — 80/443 plus an explicit allowlist; no internal service ports.
8. **Breaker + quarantine** — repeated blocks quarantine the feed for 24 h instead of retrying.

`ALLOW_PRIVATE_FETCH` exists for integration tests only and is refused when `ENV=production`.

## Content safety

* Feed HTML is sanitised with an allowlist (`p`, `br`, `a`, `img`, headings, lists); scripts,
  styles, iframes, forms, SVG and event handlers are removed and unsafe URL schemes rejected.
* `feed_content` is stored for reader context only. The platform never claims to republish a
  publisher's article and always links to the original.
* Displayed text is escaped in the web clients; the Android client renders text, not HTML.
* Images load from publisher CDNs over HTTPS; the clients degrade gracefully when an image fails.

## Admin surface

* Session tokens are HMAC-SHA256 signed (payload + expiry), 8 h TTL, delivered as an HttpOnly
  cookie and as a Bearer token for tooling.
* Role permissions are enforced **server-side** per endpoint (`requireAction`), not in the UI.
* Every mutation writes an audit row (`actor`, `action`, `entity`, `before`, `after`, timestamp).
* The development seed `admin/afnews-admin` exists only while `ADMIN_PASSWORD_HASH` is unset;
  production startup guards refuse to run with the default session key or an empty hash.
* Login failures are throttled and audited (`LOGIN_FAILED`).

## Rate limiting and abuse

Per-IP token buckets: 240 requests/minute baseline, ×4 headroom for search, ×3 for writes and
×6 for push registration. Push registration is additionally capped per device token.

## Data protection

* No accounts, no emails, no personal data in v1.
* Push tokens are stored as anonymous device registrations, revocable by the client
  (`DELETE /v1/push/registrations/{id}`).
* Reader preferences (language, province, saved items) never leave the device.
* Secrets come from the environment; `.env` is git-ignored and `.env.example` documents shape
  only.

## Dependency posture

Backend: Go standard library plus `pgx`, `brotli`, `x/net`, `x/crypto`, `x/text`, `x/oauth2`,
`modernc.org/sqlite`. Android: pinned in `gradle/libs.versions.toml` with API-21 ceilings
documented per entry. No analytics SDK beyond FCM/GA in the client, no ad SDKs.

# Webhook delivery can reach private networks

Created: 2026-10-06
Category: bug
Status: ready-for-agent

## Problem

`WebhookService` guards against SSRF with `isInternalURL`, which resolves the URL's host once and checks the first address. That check is bypassable four ways:

1. **Redirects.** The delivery client is a plain `http.Client`, which follows up to 10 redirects without re-checking. A public endpoint answering `302 Location: http://169.254.169.254/...` makes the server fetch the metadata service.
2. **DNS rebinding.** The check's lookup and the client's dial are separate resolutions. A host that answers public first and private second passes the check and connects to the private address. Only the first address of a multi-address answer is checked at all.
3. **Retries and redelivery.** `retryDeliver`, used by `RetryPending` and `RedeliverByID`, never runs the check. A webhook created while its host resolved publicly is retried and redelivered to wherever the host points now.
4. **Error text as an oracle.** Failed deliveries store `err.Error()`, which a repo manager reads in the delivery list. Through the holes above, the difference between "connection refused", a timeout and a response code maps the internal network.

The check also misses addresses the import guard blocks: unspecified, `0.0.0.0/8`, CGNAT `100.64.0.0/10` (where some cloud metadata services live) and multicast. A DNS failure at create time is treated as public.

The repo-import guard in `internal/service/import_guard.go` already closes all of this for go-git: it resolves at dial time, refuses if any address is private, dials only the vetted addresses, re-dials redirects through the same check and bypasses proxies.

## Fix sketch

- Move the address policy into `internal/service/netguard.go`: `blockedIP`, a `PrivateNetworkError{Host}` (replacing `ImportBlockedError`), `resolvePublic(ctx, host)` and `dialPublic(ctx, dialer, network, addr)`. The import guard keeps its own bookkeeping and calls `dialPublic`.
- `newWebhookHTTPClient(allowLocal)`: `DialContext` goes through `dialPublic` unless `allowLocal`, `Proxy` is nil (through a proxy the dial check would vet the proxy), and `CheckRedirect` returns `http.ErrUseLastResponse`. A 3xx is recorded like any other non-2xx and retried.
- One `send` path for first delivery, retry and redelivery, so every attempt dials through the guard. A delivery refused by the guard is recorded with `PrivateNetworkError`'s message and no further retry: the outcome won't change until the host's DNS does.
- `Create` validates the URL with `checkWebhookURL`: http or https, a host, and (unless local networks are allowed) no private address in the host's current answer. Failures are a `WebhookURLError`, which the handler answers with 400 and the reason (or the form error for HTMX), instead of 500.
- New `webhook.allow_local_networks` (`CZ_WEBHOOK_ALLOW_LOCAL_NETWORKS`), default `false`, separate from `import.allow_local_networks`: an admin who imports from a LAN git server doesn't necessarily want every repo manager posting into the LAN.
- `RetryPending` waits for the deliveries it started, so a tick finishes before the next one lists pending rows.

## Acceptance criteria

- [ ] Delivery to a loopback, private, link-local, CGNAT or unspecified address is refused at dial time, on first delivery, retry and redelivery alike, including a hostname that resolves to one.
- [ ] Redirects are not followed; the 3xx is recorded and retried.
- [ ] A refused delivery is recorded with an error that names the host, not an address, and is not retried.
- [ ] Deliveries don't go through `HTTP_PROXY`/`HTTPS_PROXY`.
- [ ] Creating a webhook with a non-http(s) URL, no host, or a host resolving to a private address answers 400 with the reason.
- [ ] `webhook.allow_local_networks: true` allows local targets for both the create check and delivery.
- [ ] `docs/webhooks.md`, `docs/configuration.md` and `docs/access-control.md` describe the guard and the knob.

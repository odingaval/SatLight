# LNURL-pay callback URL is fetched without SSRF validation

**Status: fixed and verified in working tree (`make test`, `make test-int` green). Do not file.**

Labels: security,bug

Priority: Critical (worker SSRF via attacker-controlled wallet domain).

## Problem

`invoiceForDestination` fetches the LNURL `callback` URL verbatim. A malicious
Lightning-address server can point the callback at internal hosts (link-local
metadata, intranet services) and the payout worker will GET it during a payout.

## Evidence

- [service.go](/home/bocal/SatLight/internal/payout/service.go:201) parses and
  fetches `params.Callback` with no scheme/host/IP checks.

## Acceptance criteria

- [ ] Callback restricted to `https` on public IPs (no localhost, RFC1918,
      link-local, or DNS-rebinding targets); redirect policy locked down.
- [ ] Existing 10s timeout and 1MB body cap kept; unit tests cover evil callback,
      redirect-to-private-IP, and non-HTTPS rejection.

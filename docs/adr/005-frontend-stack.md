# ADR-005: Frontend stack

- **Status:** Accepted
- **Date:** 2026-10-22

## Context
The UI is mostly forms, lists, and status pages. Accessibility and speed matter more than rich interactivity. The M1 landing page needs a small frontend that can call the Go API and work independently during local development.

## Decision
Use Vite with TypeScript and static HTML for the web frontend. Use htmx for progressive server-driven interactions where useful; avoid a client-side framework until product needs justify one.

## Consequences
Vite provides a small development server and production bundle while keeping UI code close to the Go API contracts. The browser app is served separately in local development and proxies `/api` to the Go server.

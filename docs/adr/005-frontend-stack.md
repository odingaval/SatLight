# ADR-005: Frontend stack

- **Status:** Open
- **Date:** 2026-10-21

## Context
The UI is mostly forms, lists, and status pages. Accessibility and speed matter more than rich interactivity.

## Decision
TBD by the developer and designer. Recommended: server-rendered Go templates with htmx, or SvelteKit. Record the choice here.

## Consequences
Server-rendered keeps the stack small; a SPA gives richer interaction at higher complexity.

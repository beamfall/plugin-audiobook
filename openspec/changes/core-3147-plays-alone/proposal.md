# Audiobooks play alone

Ticket: CORE-3147 (`queue:core:main`). Governing ADR: ADR-0055 §2.4, Amendment B.

## Why

ADR-0055 §2.4 says a book never auto-advances into the next one. Clients still ran one book into
the next from library shelves, See all, a standalone book's Play and person pages, because nothing
on the wire said a book should play alone. Plugin SDK v0.4.11 adds `Engagement.PlaysAlone` for
that; core passes it to clients as `playsAlone`.

## What Changes

- The bundle sets `Engagement.PlaysAlone`, next to `ResumeMedia`.
- `go.mod` pins plugin SDK v0.4.11. The declared SDK range stays `[0.4.7, 0.4.7]`: the SDK's
  `Version` identity is still 0.4.7 at v0.4.11.

## Rollback

Revert the commit and retag; core keeps its previous pin until it repins.

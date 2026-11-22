# Next in Series home shelf

Ticket: CORE-3013 (`queue:core:main`).

## Why

A listener who finishes book 3 of a series has no prompt for book 4 on the home Layout.
Continue Listening only holds books with a resume point. Core's generic `next-up` Source
already returns, for each ordered container the Profile has engagement in, the first
unwatched child in play-next order. Series sort their books by the `contains-book`
`sequence` attribute, so that Source is the next book in each series.

## What Changes

- The home Layout gains a "Next in Series" shelf (`next-in-series`, poster art,
  Source `next-up`) right after Continue Listening.
- Nothing auto-plays (ADR-0055 §2.4): the shelf only lists the next book. Starting it is a
  manual choice.

## Impact

- `bundle.go` home Layout only. No schema, parser or hydration change.

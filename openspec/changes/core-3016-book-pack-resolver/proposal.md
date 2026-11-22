# Offline pack resolver for one book

Ticket: CORE-3016 (`queue:core:main`).

## Why

Core packs whole books offline: every Part of the book's one Media and its chapter Markers. Core
names no domain terms, so the pack kind for a book and its resolution rule live here, as the Music
and Film & TV plugins do for theirs.

## What Changes

- `PackKindBook = "audiobook-book"` and `PackResolvers()`, keyed by kind.
- The book resolver takes one Media id (a book is one Media), checks it is visible through the
  reader core binds to the caller's Library filter, and labels the pack "Audiobook".

## Non-goals

- No Parts or Markers logic here: core packs every Part of the resolved Media and its chapters.
- No change to the bundle, parser or home layout.

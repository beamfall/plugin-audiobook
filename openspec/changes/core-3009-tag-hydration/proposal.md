# Map embedded tags and folders to credits, series and genres

Ticket: CORE-3009 (`queue:core:main`).

## Why

Audiobook Person, Series and Genre Entities are filled only by scraper matches, so a well-tagged
library shows books without authors, narrators or series until identification runs, and the open
scrapers carry no narrator at all. The tags already hold this information. The plugin owns the
audiobook vocabulary, so it owns the mapping; core writes the relations through its hydration seam.

## What Changes

- New `MapHydration(tags, path) Hydration` returns plugin-owned plan types
  (`Hydration`, `HydrationRelation`, `HydrationEntity`): Person `credited-on` credits with a `role`
  attribute, Series `contains-book` with a normalised `sequence` attribute and sort order
  (sequence x 1000, rounded), and Genre `genre-of` links. Tag precedence and splitting are in the
  spec delta.
- The bundle declares the `audiobook-hydrated` flag Media field (`HydratedField`) so core
  hydrates each Media once.
- The bundle's Kind, relation and attribute names become exported constants that the bundle and
  the mapper share.
- No core imports; the bundle Version stays 0.1.4, because a bump needs SDK producer evidence
  and the Kinds and relations are unchanged.

## Non-goals

- Writing anything: core's adapter writes the plan, additively, and checks names against the
  active bundle.
- Re-hydrating after a re-tag, name-order folding and title folding (later tickets).
- An author folder fallback: without the library root, "Library/Title/" and "Author/Title/" look
  the same, so the author folder is never used.

## Failure modes

- A name joined with " and " or "/" ("Penn and Teller", "AC/DC") splits into two people; the
  operator merges them. Commas are never split, so "Last, First" stays one person.
- A series folder ending in an unrelated number ("Live at Wembley 95") reads as a series, the
  parser's existing known heuristic limitation; a series tag wins.
- A non-numeric sequence tag is kept as written, with no sort order.

## Acceptance evidence

`go test ./...`: `TestMapHydration`, `TestMapHydrationOrdersCreditsSeriesGenres`,
`TestNormaliseSequence`, `TestHydratedFieldDeclared`, `TestHydrationVocabularyDeclared`;
`script/gate.sh`.

## Rollback

Revert and retag; core keeps pinning the previous release until it repins.

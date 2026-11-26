# Accept partial/untagged audiobook folders and clean part titles

Tickets: CORE-3480, CORE-3481 (`queue:core:main`).

## Why

Multi-file audiobook folders (for example `Elton John - Me/Part01.mp3..PartNN.mp3`, partial-tag
rips, or flat author folders) are dropped whole whenever one file lacks an `album`+`track` pair
or fails the folder-title proof. Separately, multi-Part books without embedded chapter atoms
produce raw or blank `PartMarkerTitle` labels for bare ordinal stems (`Part_01.mp3`, `01.mp3`).

## What Changes

- Add `ParseDirectory(dir, probes, hasMediaSubdirs)` so the scanner can resolve all non-image
  audio files of a leaf folder together:
  - Refuse the folder with `RefusalConflictingAlbumTags` only when two or more distinct `album`
    tags with no shared book exist in the folder.
  - Accept every non-image audio file in any non-conflicting leaf folder as ordered Parts of one
    book, even with partial tags, missing tags, unnumbered tracks alongside numbered parts, or
    author prefixes / format noise suffixes in the folder name.
  - Order Parts by `disc`+`track` tags first, then leading filename number, then natural filename
    sort.
- Allow `ParseWithContext` to accept partial tags when the missing piece is supplied by the
  folder name or filename ordinal, and read container `disc` / `discnumber` / `tpos` tags in
  addition to `track` / `tracknumber` / `trck`.
- Have `PartMarkerTitle` fall back to the cleaned filename (or `Part N` for bare numbers) when
  stripping a leading ordinal leaves nothing.

## Non-goals

- Reading sibling directories from inside the plugin (the plugin stays pure stdlib + SDK; core
  supplies `hasMediaSubdirs`).
- Merging loose files directly in a folder that contains disc/book subdirectories (`scan-part-groups`).
- Changing single-file `Parse` heuristics for digit-containing titles (`1984`, `100 Years of Solitude`).

## Failure modes

- A folder containing files from two unrelated books (distinct non-matching `album` tags) is
  refused with `RefusalConflictingAlbumTags`.
- A folder with media subdirectories leaves loose files on `ParseWithContext` so parent folders of
  multi-disc rips do not absorb stray files.

## Acceptance evidence

`script/gate.sh`, `script/gate.sh race`, `openspec validate --all --strict`, and new tests in
`parse_test.go` and `conformance_test.go` covering partial tags, author-prefixed and noise-suffixed
folders, `disc`+`track` ordering, natural filename ordering, conflicting album refusal, and
`PartMarkerTitle` fallbacks.

## Rollback

Revert this change and retag; core keeps pinning the previous release until repinned.

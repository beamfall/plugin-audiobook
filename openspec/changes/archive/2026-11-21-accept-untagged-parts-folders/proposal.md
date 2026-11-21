# Accept a folder of numbered untagged parts as one audiobook

Ticket: CORE-1834 (`queue:core:main`).

## Why

Since CORE-1710, core hands every file of a multi-file folder to the plugin with
`DirectoryMediaFileCountMultiple` context and refuses the whole folder when the plugin
refuses any audio file in it. Plugin v0.1.6 accepts a multi-file part only when it has
both an `album` and a positive `track` tag, so a folder of numbered but untagged parts
("01.mp3", "Part 2.mp3", "Part 10.mp3") is refused and shows only in Unmatched.

## What Changes

- `ParseWithContext` also accepts a file in a multi-file folder that has neither an
  `album` nor a `track` tag when its filename starts with a positive part number and
  the folder names the book. The result is the existing `Parse` result: Title is the
  folder name, Index is the filename number, PartGroup is the folder.
- Part order is the numeric Index, which core already sorts on, so "Part 2" plays
  before "Part 10".
- New `ContextRefusalReason(path, probe, context)` returns the reason a file is
  refused (or ""), using exported reason constants, so core can show it later through
  a consumer-owned interface as it does `PartMarkerTitle`.
- No SDK change: the plugin still sees one file and the bounded directory count.

## Non-goals

- Reading sibling files from the plugin (the SDK context deliberately carries no
  sibling names). Duplicate numbers in one folder are not detected here.
- Changing tagged behaviour: a file with an `album` or `track` tag still needs both.

## Failure modes

- A folder mixing numbered parts with an unnumbered file: the unnumbered file is
  refused (`RefusalUntaggedNoPartNumber`) and core refuses the folder.
- A flat author folder (author tag matches the folder): each file would be titled from
  its own name, so it is refused (`RefusalFolderNotBookTitle`).
- Digit titles ("1984.mp3") beside other files read as part numbers, the existing
  known heuristic limitation.

## Acceptance evidence

`go test ./...`: `TestUntaggedNumberedPartsFolderIsOneBook`,
`TestUntaggedPartsSortNaturallyNotLexically`,
`TestAmbiguousMultiFileFolderStillRefusedWithReason`, `TestTaggedPartsFolderUnchanged`
and the updated `TestParseWithContextConformance` vectors. Core's scan test lands with
the core repin of the release that carries this change.

## Rollback

Revert this change and retag; core keeps pinning the previous release until repinned.

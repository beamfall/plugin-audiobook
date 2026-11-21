# Disc and CD subfolders join one book

Ticket: CORE-3017 (NEW-AB10, `queue:core:main`).

## Why

A multi-disc rip is laid out as `Author/Title/Disc 1/01.mp3`, `Disc 2/01.mp3`. The parser
treated each disc folder as its own book: PartGroup was the disc folder, the fallback title
was "Disc 1", part numbers restarted on every disc, and the series position was read from
the book folder instead of the folder above it. Core groups parts by PartGroup, class and
title, so one book became one Media per disc.

## What Changes

- A leaf folder named `disc`, `disk` or `cd` (any case), an optional single space, `.`, `_`
  or `-`, and a number of up to three digits is a disc subfolder when its parent is a real
  folder. Its parent is then the book folder.
- PartGroup is the book folder and the fallback title is the book folder name (the `album`
  tag still wins), so every disc's parts share core's grouping key.
- Index is `disc * 10000 + track`, so core's numeric sort plays disc then track. The stride
  exceeds every filename ordinal (four digits); a track tag of 10000 or more is not a valid
  ordinal inside a disc folder. A single unnumbered file in a disc folder takes Index
  `disc * 10000`. IndexEnd (a per-disc total) is dropped for disc-qualified parts.
- The series position is read from the folder above the book folder, as before for
  `Author/Series ##/Title/`.
- The multi-file refusals (CORE-1710, CORE-1834) judge the book folder, and an untagged file
  beside other files in a disc folder still needs its own part number.
- `PartMarkerTitle` prefixes a filename-derived label with the disc ("Disc 2 · The Door")
  and labels a bare-number file "Disc 2 · Part 3". An embedded `title` tag is unchanged.

## Non-goals

- Reading the `disc` tag, or merging discs ripped into one folder with restarting tracks.
- `Part N` or `Volume N` folders: they stay separate books.
- No core or SDK change: core already groups consecutive parts by PartGroup, class and
  title, and sorts the group by Index.

## Failure modes

- A disc folder directly under the library root takes the root folder's name as its title,
  since the plugin cannot see the root. The `album` tag avoids it.
- Core refuses a folder per directory, so a refused disc folder leaves the other discs
  imported as a book missing that disc.

## Acceptance evidence

`GOWORK=off go test ./...`: `TestDiscSubfoldersJoinOneBook`,
`TestDiscFolderLookalikesDoNotMerge`, `TestDiscFolderRefusals` and the disc cases in
`TestPartMarkerTitle`.

## Rollback

Revert this change and retag; core keeps pinning the previous release until repinned.

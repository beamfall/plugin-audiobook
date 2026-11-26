## MODIFIED Requirements

### Requirement: Untagged numbered parts folder folds into one book

In a multi-file leaf folder, the parser SHALL group all non-image audio files without conflicting
`album` tags as one ordered book in `ParseDirectory`. In per-file `ParseWithContext`, numbered
untagged parts and partial tags whose filename or folder supplies the missing title or part ordinal
SHALL be accepted, while unnumbered files without a book+track proof or explicit ordinal stay
refused.

#### Scenario: Numbered untagged parts

- **WHEN** a multi-file directory holds untagged audio files whose filenames start with a positive part number (`01.mp3`, `Part 2.mp3`, `Chapter 10 - Name.mp3`, `Part01.mp3`)
- **THEN** all files are accepted and fold into one `Media` ordered numerically across disc and part numbers

#### Scenario: Unrelated file beside the parts

- **WHEN** per-file `ParseWithContext` inspects an untagged, unnumbered file (`Harbour Interview.mp3`) or an isolated `album`-only unnumbered file (`Chapter.m4b`) in a multi-file folder (or `hasMediaSubdirs` fallback)
- **THEN** `ParseWithContext` refuses the file (`ok = false`) and `ContextRefusalReason` explains what is missing

#### Scenario: Tagged parts

- **WHEN** a multi-file folder carries full or partial `album`, `track`, and `disc` tags without conflicting book titles
- **THEN** all non-image audio files are accepted in `disc`, `track`, then natural filename order

## ADDED Requirements

### Requirement: Directory-wide grouping tolerates partial tags, author folders, and noise

`ParseDirectory` SHALL inspect all non-image audio files of a flat directory together and emit one
grouped `Media` (`Class = "audiobook"`, shared cleaned `Title`, sequential `Index >= 1`,
`PartGroup`) unless two or more files declare distinct non-empty `album` tags belonging to different
books.

#### Scenario: Partial tags and author-prefixed or noise-suffixed folders

- **WHEN** a flat directory such as `Elton John - Me (Unabridged) [MP3]` holds files with partial `album`/`track` tags, untagged files, or `Disc N` suffixes on matching `album` tags
- **THEN** `ParseDirectory` emits all files under one grouped book with a cleaned title ordered by `disc`+`track` tags then natural filename sort

#### Scenario: Conflicting album tags in one folder

- **WHEN** a flat directory holds files declaring two or more distinct `album` tags (`Book One` vs `Book Two`)
- **THEN** `ParseDirectory` refuses the directory with `RefusalConflictingAlbumTags`

### Requirement: Part marker title cleans bare ordinal filenames

Outside disc subfolders, when a Part has no `title` tag and stripping its leading ordinal leaves no
remaining words, `PartMarkerTitle` SHALL return a clean ordinal label (`Part 1`, `Chapter 3`)
instead of an empty string.

#### Scenario: Bare part filename without title tag

- **WHEN** `PartMarkerTitle` is called for `Part01.mp3`, `Part_01.mp3`, or `01.mp3` with no `title` tag
- **THEN** it returns `Part 1`

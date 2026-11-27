## ADDED Requirements

### Requirement: Disc markers in album tags and filenames unify books

Disc markers (`Disc N`, `[Disc N]`, `CD N`, `Disk N`, `Tape N`) SHALL be stripped from `album` tags
and filenames before computing book identity and SHALL supply the Part's disc ordinal when no disc
folder or `disc` tag is present.

#### Scenario: Mortality disc tags

- **WHEN** files in a book folder carry `album` tags `Mortality - Unabridged - [Disc 1]` and `Mortality - Unabridged - [Disc 2]`
- **THEN** both share the title `Mortality` and order by disc then track

### Requirement: Wrapper subfolders flatten onto the parent book folder

A child folder named `Audiobook`, `Audio`, `MP3`, or `M4B` (case-insensitive), or a child folder
whose cleaned title matches its parent folder's cleaned title, SHALL flatten onto the parent book
folder.

#### Scenario: Tower wrapper layouts

- **WHEN** files sit under `A Woman First/Audiobook/`, `Chaos(Audiobook)/Chaos/`, `I, Partridge/Audiobook/`, or `Josh Peck/Audiobook/`
- **THEN** each yields one book titled from its parent folder

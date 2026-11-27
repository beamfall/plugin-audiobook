# Disc markers, wrapper folder flattening, and credit/genre hygiene

Tickets: CORE-3482, CORE-3485 (`queue:core:main`).

## Why

1. Multi-disc books whose disc number sits in the `album` tag or filename (`Mortality - Unabridged - [Disc 1]` / `[Disc 2]`) split into separate items, and wrapper folders (`Audiobook`, `Audio`, `MP3`, `M4B`, or a child folder equal to the parent's title such as `Chaos(Audiobook)/Chaos`) are not flattened onto the parent book folder.
2. Embedded tag hydration did not decode HTML entities before splitting, left comma-joined names unsplit, kept role suffixes (`- foreword`, `(narrator)`) and `full cast` as names, credited composers as authors when distinct authors existed, and did not split comma-joined genres or drop junk stopwords (`Audiobook`, `Audiobooks`, `Hörbuch`, `Other`, `Unknown`).

## What Changes

- Strip disc markers (`Disc N`, `[Disc N]`, `CD N`, `Disk N`, `Tape N`) from `album` tags and filenames before computing book identity and extract them as the Part disc number.
- Flatten single wrapper child folders (`Audiobook`, `Audio`, `MP3`, `M4B`, or matching cleaned parent title) onto the parent book folder in `bookDir`.
- Decode HTML entities before splitting in `MapHydration`, split people on commas with a `Last, First` guard, extract/drop role suffixes and non-person placeholders, separate `composer` into `narrator` when distinct authors are present, and split genres on commas while dropping junk stopwords.

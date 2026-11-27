## ADDED Requirements

### Requirement: Credit and genre hygiene in tag hydration

`MapHydration` SHALL decode HTML entities before splitting, split people on commas unless the chunk
is a single `Last, First` pair, extract role suffixes or drop non-person placeholders such as
`full cast`, map `composer` to `narrator` without crediting them as `author` when distinct authors
are present, and split genres on commas while dropping junk stopwords (`Audiobook`, `Audiobooks`,
`Hörbuch`, `Other`, `Unknown`).

#### Scenario: Tower credit and genre hygiene

- **WHEN** tags carry `&#169; 5 minutes`, `Brian Christian, Tom Griffiths`, `Jay Wiseman - foreword`, `full cast`, `composer` distinct from `author`, or `Audiobook, Business &amp; Careers, Management &amp; Leadership`
- **THEN** entities decode cleanly, people and genres split on commas, `foreword` becomes the credit role, `full cast` is dropped, `composer` is credited as `narrator` only, and junk genres are omitted

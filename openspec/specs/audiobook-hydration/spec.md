# audiobook-hydration Specification

## Purpose
How the audiobook plugin maps a book's embedded tags and folder names to Person credits, Series
membership and Genre links that core writes once per Media.

## Requirements

### Requirement: Person credits from tags

`MapHydration(tags, path)` SHALL read tag keys case-insensitively and map author (`album_artist`,
else `artist`, else `author`), narrator (`narrator`, else `composer`) and translator
(`translator`) to Person `credited-on` relations with a `role` attribute. Names SHALL be split on
`;`, `/`, " & " and " and " (never commas), trimmed and deduplicated case-insensitively. The
author folder SHALL NOT be used.

#### Scenario: Multi-value author tag

- **WHEN** `album_artist` is "Douglas Preston & Lincoln Child"
- **THEN** the plan holds author credits for "Douglas Preston" (sort order 0) and "Lincoln Child" (sort order 1)

### Requirement: One credit per person

A person in several roles SHALL get one relation whose role lists them in the order author,
narrator, translator, joined with ", ". Credits SHALL carry their first-appearance position,
authors first, as sort order.

#### Scenario: Self-narrating author

- **WHEN** `artist` is "Tina Fey" and `narrator` is "tina fey"
- **THEN** the plan holds one Person credit for "Tina Fey" with role "author, narrator"

### Requirement: Series membership

Series SHALL come from `series`, else `mvnm`, else `grouping`, and sequence from `series-part`,
else `mvin`; the parser's "Series ##" grandparent folder SHALL supply either when no tag does.
The Series `contains-book` relation's `sequence` attribute drops leading zeros and trailing
fractional zeros; its sort order is round(sequence x 1000) when the sequence is a decimal number.

#### Scenario: Fractional sequence

- **WHEN** `series` is "The Stormlight Archive" and `series-part` is "4.50"
- **THEN** the Series relation has sequence "4.5" and sort order 4500

#### Scenario: Folder fallback

- **WHEN** there are no tags and the path is "Brandon Sanderson/Mistborn 03/The Hero of Ages/01.mp3"
- **THEN** the plan holds a Series "Mistborn" relation with sequence "3" and no Person credit

### Requirement: Genres and the mapped flag

Genres SHALL come from `genre`, split on `;` and `/`, as Genre `genre-of` relations. `Mapped`
SHALL be true exactly when the plan holds a relation.

#### Scenario: Nothing to map

- **WHEN** the tags are empty and the path has no series folder
- **THEN** the plan holds no relation and `Mapped` is false

### Requirement: Hydration marker field

The bundle SHALL declare the flag Media field `audiobook-hydrated` (`HydratedField`), which core
sets once it has written a Media's hydration plan.

#### Scenario: Field declared

- **WHEN** core loads the bundle
- **THEN** its Media fields include `audiobook-hydrated` of type flag

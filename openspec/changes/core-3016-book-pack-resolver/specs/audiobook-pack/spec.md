## ADDED Requirements

### Requirement: A book pack resolves to its one Media

The plugin SHALL export a pack resolver for kind `audiobook-book` that resolves a target ref naming
exactly one visible Media id to that Media with the label "Audiobook". It SHALL refuse an empty
ref, a ref naming more than one id, and an id the caller cannot see.

#### Scenario: Visible book

- **WHEN** the ref names one Media visible to the caller
- **THEN** the resolver returns that Media id and the label "Audiobook"

#### Scenario: Hidden book

- **WHEN** the ref names a Media the caller cannot see
- **THEN** the resolver returns an error and no Media

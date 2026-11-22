## ADDED Requirements

### Requirement: Next in Series shelf

The home Layout SHALL include a shelf with ID `next-in-series`, titled "Next in Series"
("Suite de la série" in French), with poster artwork and the `next-up` Source, placed directly
after the Continue Listening shelf. The shelf SHALL only list books; it SHALL NOT start
playback on its own.

#### Scenario: Home Layout order

- **WHEN** the bundle's home Layout is read
- **THEN** its first two sections are `continue-listening` and `next-in-series`, and
  `next-in-series` uses the `next-up` Source

## ADDED Requirements

### Requirement: Untagged numbered parts folder folds into one book

In a multi-file folder, the parser SHALL accept a file that has neither a book (`album`) nor a
part ordinal (`track`) tag when its filename starts with a positive part number and its folder
names the book, returning the folder name as Title, the number as Index and the folder as
PartGroup. Files with either tag SHALL still need both. Every other file in a multi-file folder
SHALL be refused, and `ContextRefusalReason` SHALL name the missing evidence.

#### Scenario: Numbered untagged parts

- **WHEN** a folder holds untagged "01.mp3", "Part 2.mp3" and "Part 10.mp3"
- **THEN** each is accepted with the folder name as Title and Index 1, 2 and 10, so the parts order 1, 2, 10

#### Scenario: Unrelated file beside the parts

- **WHEN** a multi-file folder also holds an untagged file whose name does not start with a number
- **THEN** that file is refused and its refusal reason says it has no part number

#### Scenario: Tagged parts

- **WHEN** a multi-file folder's files carry an `album` tag and a positive `track` tag
- **THEN** they are accepted and ordered by the track tag as before

## ADDED Requirements

### Requirement: Disc subfolders fold into one book

The parser SHALL treat a leaf folder named `disc`, `disk` or `cd` (case-insensitive) followed
by an optional separator and a number as a disc subfolder of its parent folder. Files in a disc
subfolder SHALL take the parent folder as PartGroup, the `album` tag or else the parent folder
name as Title, the series position from the folder above the parent, and Index `disc * 10000 +
track` so parts order by disc and then track. Folders with any other name SHALL keep their own
PartGroup.

#### Scenario: Multi-disc rip

- **WHEN** a book folder holds untagged "Disc 1/01.mp3", "Disc 1/02.mp3" and "Disc 2/01.mp3"
- **THEN** each has the book folder as PartGroup and its name as Title, with Index 10001, 10002 and 20001

#### Scenario: Series folder above a disc rip

- **WHEN** the path is "Author/Saga 04/Title/Disc 1/01.mp3"
- **THEN** the Sequence is "04" and the Title is "Title"

#### Scenario: Lookalike folders

- **WHEN** a folder is named "Part 1" or "Disc 1 Bonus"
- **THEN** it is its own PartGroup and Title

#### Scenario: Disc part marker

- **WHEN** an untagged part is "Disc 2/03.mp3"
- **THEN** its part marker title is "Disc 2 · Part 3"

## ADDED Requirements

### Requirement: Disc subfolders fold into one book

The parser SHALL treat a leaf folder named `disc`, `disk` or `cd` (case-insensitive) followed
by an optional separator and a number from 1 to 999 as a disc subfolder of its parent folder.
Files in it SHALL take the parent folder as PartGroup, the parent folder name as Title whatever
the `album` tag says, and the series position from the folder above the parent. Folders with any
other name SHALL keep their own PartGroup.

#### Scenario: Multi-disc rip

- **WHEN** a book folder holds untagged "Disc 1/01.mp3", "Disc 1/02.mp3" and "Disc 2/01.mp3"
- **THEN** each has the book folder as PartGroup and its name as Title, with Index 10001, 10002 and 20001

#### Scenario: Series folder above a disc rip

- **WHEN** the path is "Author/Saga 04/Title/Disc 1/01.mp3"
- **THEN** the Sequence is "04" and the Title is "Title"

#### Scenario: Per-disc album tags

- **WHEN** "Book/Disc 1/a.mp3" is tagged album "Novel (Disc 1)" and "Book/Disc 2/a.mp3" album "Novel (Disc 2)"
- **THEN** both have Title "Book" and PartGroup the book folder

#### Scenario: Lookalike folders

- **WHEN** a folder is named "Part 1", "Disc 0" or "Disc 1 Bonus"
- **THEN** it is its own PartGroup and Title

### Requirement: Disc parts order by disc then track

A part in a disc subfolder SHALL take Index `disc * 10000 + track`. A lone file in a disc folder
SHALL take Index `disc * 10000` from the path alone, so a rescan without tags keeps it.

#### Scenario: Lone file per disc

- **WHEN** "Book/CD2/Book.mp3" is the only file in its folder
- **THEN** its Index is 20000 with or without tags

#### Scenario: Disc part marker

- **WHEN** an untagged part is "Disc 2/03.mp3"
- **THEN** its part marker title is "Disc 2 · Part 3"

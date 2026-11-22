# audiobook-playback Specification

## Purpose
How an audiobook plays: as one Media that resumes mid-play and never runs on into another item.

## Requirements

### Requirement: An audiobook plays alone

The audiobook bundle SHALL declare `Engagement.PlaysAlone`, so a host tells clients that a book
starts with a one-item queue and never auto-advances into another book or out of itself. It SHALL
keep declaring `Engagement.ResumeMedia`, because a book resumes mid-play.

#### Scenario: Bundle engagement

- **WHEN** core reads the audiobook bundle's engagement posture
- **THEN** `PlaysAlone` is true and `ResumeMedia` is true

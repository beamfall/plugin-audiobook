// Package audiobook is the first-party Audiobook domain plugin for Beamfall
// Core: a purely declarative bundle — Entity Kinds, a home Layout, and
// official/open scrapers — written against the fixed vocabularies the plugin
// SDK exposes. It adds no schema and no query strings. The audio media class,
// resume eligibility, and the engagement-backed Source kinds it leans on
// already live in Core; this bundle only uses them. Audiobook vocabulary lives
// here, in an independent plugin module, rather than in Core; the matching
// multi-file parser hook lives alongside this bundle in parse.go.
//
// Entity model: an audiobook is ONE Media — "Audiobook" is a display label,
// never a schema identifier, and never a container of chapter-Media. A
// multi-file book is that one Media's ordered Parts; chapters are Markers. So
// "Book" is NOT an Entity Kind. The Kinds are: one Person archetype for all
// humans (Author/Narrator are credit-relation attributes, never separate
// Kinds — the self-narrating-author case); Series, an ordered container whose
// sequence is a relation attribute that MUST allow fractional order ("Book
// 4.5" — carried as a string relation attribute, since relation attributes are
// free-form strings and need no schema); and a flat Genre category. Publisher
// is a Media field, not a Kind.
package audiobook

import sdk "github.com/beamfall/plugin-sdk"

const openLibraryMaxAuthorLookups = 8

// Bundle returns the Audiobook plugin's declarative contribution.
func Bundle() sdk.Bundle {
	return sdk.Bundle{
		ID:          "audiobook",
		Version:     "0.1.4",
		SDKFloor:    "0.2.0",
		SDKCeiling:  "0.3.2",
		DisplayName: sdk.LocaleString{"en": "Audiobooks", "fr": "Livres audio"},
		// An audiobook is one Media — display label only, never a schema
		// identifier.
		MediaLabel:   sdk.LocaleString{"en": "Audiobook", "fr": "Livre audio"},
		MediaClasses: []string{"audio"},
		// Audiobooks resume mid-play, like podcast episodes — the one place the
		// audio no-resume default is wrong (resume eligibility is declarative,
		// not a class convention). One book is one logical timeline across its
		// Parts, so one resume position.
		Engagement: sdk.Engagement{ResumeMedia: boolPtr(true)},
		// Media (the book) fields. Narrator is tags-primary (tags are the
		// dependable narrator source) — the open scrapers below carry no
		// narrator — so it lands as a Media field the scan-side tag reader fills;
		// it is NOT a Person credit relation here because the open scrapers
		// cannot hydrate it and the embedded tag is a flat string. ISBN/ASIN are
		// identification keys the scrapers look up against.
		MediaFields: []sdk.FieldSpec{
			{Name: "narrator", Type: "text", Filterable: true,
				Label: sdk.LocaleString{"en": "Narrator", "fr": "Narrateur"}},
			{Name: "publisher", Type: "text",
				Label: sdk.LocaleString{"en": "Publisher", "fr": "Éditeur"}},
			{Name: "isbn", Type: "text",
				Label: sdk.LocaleString{"en": "ISBN", "fr": "ISBN"}},
			{Name: "release-date", Type: "date", Sortable: true,
				Label: sdk.LocaleString{"en": "Release Date", "fr": "Date de sortie"}},
		},
		EntityKinds: []sdk.EntityKind{
			{
				// One Kind for all humans: Author and Narrator
				// are credit-relation attributes, never separate Kinds — a
				// self-narrating author (Tina Fey) is ONE Person with two credit
				// relations. The Person browse split "Wrote"/"Narrated" is the
				// relation `role` attribute, the Music Artist "Albums"/"Appears
				// on" precedent.
				Name:        "person",
				DisplayName: sdk.LocaleString{"en": "Person", "fr": "Personne"},
				Archetype:   "person",
				Fields: []sdk.FieldSpec{
					{Name: "sort-name", Type: "text",
						Label: sdk.LocaleString{"en": "Sort Name", "fr": "Nom de tri"}},
					{Name: "biography", Type: "text", Searchable: true,
						Label: sdk.LocaleString{"en": "Biography", "fr": "Biographie"}},
				},
				Relations: []sdk.RelationSpec{
					// Person → Media credit. The `role`
					// attribute carries "author" | "narrator" | "translator" —
					// one relation, the attribute distinguishes the credit, so a
					// self-narrating author is two rows on one Person, never a
					// duplicate entity. "credit is a relation, never an Entity."
					{Name: "credited-on", ToKind: sdk.MediaKindName, Attributes: []string{"role"}},
				},
			},
			{
				// Series: ordered Book Media. OrderedChildren carries the "Next
				// in series" walk (no auto-advance — a client affordance, not
				// structure). The per-book sequence is a relation attribute that
				// MUST allow fractional order ("Book 4.5" novellas): the `sequence` attribute below is a
				// free-form string, so "4.5" is representable with zero schema —
				// the integer sort_order column (OrderedChildren) is the coarse
				// play order; the fractional sequence is the displayed/sorted
				// book number the client orders by.
				Name:            "series",
				DisplayName:     sdk.LocaleString{"en": "Series", "fr": "Série"},
				Archetype:       "container",
				OrderedChildren: true,
				Fields: []sdk.FieldSpec{
					{Name: "summary", Type: "text", Searchable: true,
						Label: sdk.LocaleString{"en": "Summary", "fr": "Résumé"}},
				},
				Relations: []sdk.RelationSpec{
					// Series → Media: the per-book membership link. `sequence`
					// is the fractional book number ("1", "2", "4.5") — a string
					// attribute, never an integer, so half-numbered novellas fit.
					// The Author of a Series is derived from its
					// books' Person credits, not a separate relation.
					{Name: "contains-book", ToKind: sdk.MediaKindName, Attributes: []string{"sequence"}},
				},
			},
			{
				// Flat, like the Music Genre Kind; no genre hierarchy.
				Name:        "genre",
				DisplayName: sdk.LocaleString{"en": "Genre", "fr": "Genre"},
				Archetype:   "category",
				Relations: []sdk.RelationSpec{
					// Genre → Media: the per-book genre link scrapers hydrate.
					{Name: "genre-of", ToKind: sdk.MediaKindName},
				},
			},
		},
		// Home Layout. Audiobooks resume, so continue-listening leads —
		// unlike Music. Section titles use the locale-map form with English
		// and French strings.
		Layouts: []sdk.LayoutSpec{{ID: "home", Role: "browse", Sections: []sdk.SectionSpec{
			{
				// Backed by the Engagement.ResumeMedia override above: audiobook
				// audio stores resume positions.
				ID:          "continue-listening",
				Kind:        "shelf",
				Title:       sdk.LocaleString{"en": "Continue Listening", "fr": "Reprendre l'écoute"},
				ArtworkSlot: "poster",
				Source:      sdk.SourceSpec{Kind: "continue-watching"},
			},
			{
				ID:          "recently-added",
				Kind:        "shelf",
				Title:       sdk.LocaleString{"en": "Recently Added", "fr": "Ajouts récents"},
				ArtworkSlot: "poster",
				Source:      sdk.SourceSpec{Kind: "recently-added"},
			},
			{
				ID:          "series",
				Kind:        "grid",
				Title:       sdk.LocaleString{"en": "Series", "fr": "Séries"},
				ArtworkSlot: "poster",
				Source:      sdk.SourceSpec{Kind: "entity-items", EntityKind: "series"},
			},
			{
				ID:          "authors",
				Kind:        "grid",
				Title:       sdk.LocaleString{"en": "Authors", "fr": "Auteurs"},
				ArtworkSlot: "portrait",
				Source:      sdk.SourceSpec{Kind: "entity-items", EntityKind: "person"},
			},
			{
				ID:          "all-books",
				Kind:        "grid",
				Title:       sdk.LocaleString{"en": "All Books", "fr": "Tous les livres"},
				ArtworkSlot: "poster",
				Source:      sdk.SourceSpec{Kind: "all-items"},
			},
		}}},
		// Official/open scrapers only — no Audible, no Audnexus: Audible has no
		// public API, and an operator who wants another source adds their own
		// scraper (scrapers are declarative data). iTunes and Open Library are
		// keyless public endpoints; Google Books takes an operator-supplied API
		// key. They are book-centric and carry no narrator/duration (an accepted
		// trade-off: tags are the dependable narrator source) — so none maps the narrator
		// Media field; identification enriches title/summary/author/genre and the
		// scan-side tag reader fills narrator.
		Scrapers: []sdk.ScraperSpec{
			{
				// iTunes Search API, audiobook endpoint (media=audiobook).
				// Keyless, rate-limited. {"results": [{
				// collectionName, artistName, description, releaseDate,
				// artworkUrl100, primaryGenreName}, …]} — the search element and
				// lookup document share keys, so one FieldMap serves both; the
				// candidate id is collectionId, fed back to the lookup endpoint.
				ID:              "itunes-audiobook",
				DisplayName:     sdk.L("Apple Books (iTunes Search)"),
				Kind:            "http-json",
				Endpoint:        "https://itunes.apple.com",
				SearchTemplate:  "/search?media=audiobook&term={query}",
				CandidatesPath:  "results",
				CandidateIDPath: "collectionId",
				// GET /lookup?id={id} — same wrapping ({"results": [...]}); the
				// engine resolves the first result element against this map.
				LookupTemplate: "/lookup?id={id}",
				FieldMap: map[string]string{
					"title": "collectionName",
					// description is occasional on the audiobook endpoint.
					"summary": "description",
					// releaseDate is ISO-8601 → year transform.
					"year": "releaseDate",
				},
				Transforms: map[string]string{"year": "year-from-date"},
				ArtworkMap: map[string]string{
					// 100px square art (the only size this endpoint returns);
					// rendered square via the poster slot.
					"poster": "artworkUrl100",
				},
				// Author hydration: artistName is the book's author. genre is
				// primaryGenreName. NamePath empty means the resolved scalar IS
				// the name.
				EntityMaps: []sdk.EntityMap{
					{Kind: "person", Relation: "credited-on", Path: "artistName"},
					{Kind: "genre", Relation: "genre-of", Path: "primaryGenreName"},
				},
				// ~1 request/18s (iTunes Search API rate-limit guidance).
				MinRequestIntervalMS: 18000,
			},
			{
				// Google Books: operator API key, named by
				// AuthRef (Console config field), never embedded; travels as the
				// classic ?key= query parameter. GET /volumes?q={query} →
				// {"items": [{id, volumeInfo: {title, description, publishedDate,
				// authors:[…], categories:[…], imageLinks:{thumbnail}}}, …]}. The
				// chosen item's id feeds GET /volumes/{id} (same volumeInfo
				// shape), so one FieldMap serves both legs.
				ID:              "google-books",
				DisplayName:     sdk.L("Google Books"),
				Kind:            "http-json",
				Endpoint:        "https://www.googleapis.com/books/v1",
				AuthRef:         "google-books-api-key",
				AuthStyle:       "query:key",
				SearchTemplate:  "/volumes?q={query}",
				CandidatesPath:  "items",
				CandidateIDPath: "id",
				LookupTemplate:  "/volumes/{id}",
				FieldMap: map[string]string{
					"title":   "volumeInfo.title",
					"summary": "volumeInfo.description",
					// publishedDate is "YYYY" or "YYYY-MM-DD" → year transform.
					"year": "volumeInfo.publishedDate",
				},
				Transforms: map[string]string{"year": "year-from-date"},
				ArtworkMap: map[string]string{
					// imageLinks.thumbnail is an absolute books.google.com URL.
					"poster": "volumeInfo.imageLinks.thumbnail",
				},
				// authors and categories are arrays of plain strings — one
				// Entity per element, NamePath empty (the element scalar is the
				// name).
				EntityMaps: []sdk.EntityMap{
					{Kind: "person", Relation: "credited-on", Path: "volumeInfo.authors"},
					{Kind: "genre", Relation: "genre-of", Path: "volumeInfo.categories"},
				},
			},
			{
				// Open Library: keyless, no auth. Search is GET
				// /search.json?q={query} → {"docs": [{key, title, author_name:[…],
				// first_publish_year, subject:[…], cover_i}, …]}; the doc `key`
				// ("/works/OL123W") feeds the work lookup GET {key}.json. The
				// search docs and the work document diverge (the work document is
				// {title, description, subjects, covers:[…]}), so the search leg
				// gets its own SearchFieldMap/SearchArtworkMap.
				ID:              "open-library",
				DisplayName:     sdk.L("Open Library"),
				Kind:            "http-json",
				Endpoint:        "https://openlibrary.org",
				SearchTemplate:  "/search.json?q={query}",
				CandidatesPath:  "docs",
				CandidateIDPath: "key",
				// {key} is "/works/OL…W"; the lookup path is "{id}.json".
				LookupTemplate: "{id}.json",
				FieldMap: map[string]string{
					"title": "title",
					// The work document's description is sometimes a bare string,
					// sometimes {type, value} — the engine resolves the scalar
					// path; the object form is left unmapped rather than guessed.
					"summary": "description",
				},
				ArtworkMap: map[string]string{
					"poster": "covers.0",
				},
				// Search docs are flat: title, author_name[], first_publish_year,
				// subject[]. The search-element maps carry that divergent shape.
				SearchFieldMap: map[string]string{
					"title": "title",
					"year":  "first_publish_year",
				},
				SearchArtworkMap: map[string]string{
					"poster": "cover_i",
				},
				ArtworkURLTemplates: map[string]string{
					"poster": "https://covers.openlibrary.org/b/id/{value}-L.jpg",
				},
				// Work lookup docs carry authors as keyed /authors/OL…A refs and
				// subjects as a flat string list. The generic EntityMap reference
				// lookup fetches each author doc by key; subjects hydrate Genre
				// directly. Search author_name[]/subject[] remain display-only
				// candidate data because Apply hydrates from lookup documents.
				EntityMaps: []sdk.EntityMap{
					{Kind: "person", Relation: "credited-on", Path: "authors", LookupIDPath: "author.key", LookupTemplate: "{id}.json", MaxLookups: openLibraryMaxAuthorLookups, NamePath: "name"},
					{Kind: "genre", Relation: "genre-of", Path: "subjects"},
				},
				// 1 request/s (Open Library rate-limit guidance).
				MinRequestIntervalMS: 1000,
			},
		},
		Importers: []sdk.ImporterSpec{
			{
				ID:          AudiobookshelfImporterID,
				DisplayName: sdk.L("Audiobookshelf"),
				Categories:  []string{sdk.CategoryWatchState},
				Input:       "export-file",
				Conflict:    "additive",
			},
		},
	}
}

func boolPtr(value bool) *bool {
	return &value
}

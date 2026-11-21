package audiobook

import (
	"reflect"
	"testing"
)

func person(name, role string, order int) HydrationRelation {
	return HydrationRelation{
		From:       HydrationEntity{Kind: KindPerson, Title: name},
		Name:       RelationCreditedOn,
		Attributes: map[string]string{AttributeRole: role},
		SortOrder:  intPtr(order),
	}
}

func series(name, sequence string, order *int) HydrationRelation {
	relation := HydrationRelation{From: HydrationEntity{Kind: KindSeries, Title: name}, Name: RelationContainsBook}
	if sequence != "" {
		relation.Attributes = map[string]string{AttributeSequence: sequence}
		relation.SortOrder = order
	}
	return relation
}

func genre(name string) HydrationRelation {
	return HydrationRelation{From: HydrationEntity{Kind: KindGenre, Title: name}, Name: RelationGenreOf}
}

const flatPath = "/lib/Book.m4b"

func TestMapHydration(t *testing.T) {
	cases := []struct {
		name string
		tags map[string]string
		path string
		want []HydrationRelation
	}{
		{"album_artist wins over artist and author",
			map[string]string{"album_artist": "Brandon Sanderson", "artist": "Michael Kramer", "author": "Someone"}, flatPath,
			[]HydrationRelation{person("Brandon Sanderson", RoleAuthor, 0)}},
		{"artist when no album_artist",
			map[string]string{"artist": "Ursula K. Le Guin", "author": "Someone"}, flatPath,
			[]HydrationRelation{person("Ursula K. Le Guin", RoleAuthor, 0)}},
		{"author tag last",
			map[string]string{"author": "Terry Pratchett"}, flatPath,
			[]HydrationRelation{person("Terry Pratchett", RoleAuthor, 0)}},
		{"tag keys match case-insensitively",
			map[string]string{"ALBUM_ARTIST": "N. K. Jemisin", "Genre": "Fantasy"}, flatPath,
			[]HydrationRelation{person("N. K. Jemisin", RoleAuthor, 0), genre("Fantasy")}},
		{"narrator wins over composer",
			map[string]string{"narrator": "Kate Reading", "composer": "Other"}, flatPath,
			[]HydrationRelation{person("Kate Reading", RoleNarrator, 0)}},
		{"composer when no narrator",
			map[string]string{"composer": "Michael Kramer"}, flatPath,
			[]HydrationRelation{person("Michael Kramer", RoleNarrator, 0)}},
		{"translator",
			map[string]string{"artist": "Liu Cixin", "translator": "Ken Liu"}, flatPath,
			[]HydrationRelation{person("Liu Cixin", RoleAuthor, 0), person("Ken Liu", RoleTranslator, 1)}},
		{"author who narrates gets one merged credit",
			map[string]string{"artist": "Tina Fey", "narrator": "tina  fey"}, flatPath,
			[]HydrationRelation{person("Tina Fey", "author, narrator", 0)}},
		{"multi-value split, trimmed and deduplicated",
			map[string]string{
				"album_artist": "Douglas Preston & Lincoln Child; douglas preston",
				"narrator":     "Scott Brick / Rene Auberjonois and  Kate Reading",
			}, flatPath,
			[]HydrationRelation{
				person("Douglas Preston", RoleAuthor, 0),
				person("Lincoln Child", RoleAuthor, 1),
				person("Scott Brick", RoleNarrator, 2),
				person("Rene Auberjonois", RoleNarrator, 3),
				person("Kate Reading", RoleNarrator, 4),
			}},
		{"comma is not a separator",
			map[string]string{"artist": "Sanderson, Brandon"}, flatPath,
			[]HydrationRelation{person("Sanderson, Brandon", RoleAuthor, 0)}},
		{"series tag with mvin",
			map[string]string{"series": "Mistborn", "mvin": "02"}, flatPath,
			[]HydrationRelation{series("Mistborn", "2", intPtr(2000))}},
		{"series-part wins over mvin",
			map[string]string{"series": "Mistborn", "series-part": "3", "mvin": "2"}, flatPath,
			[]HydrationRelation{series("Mistborn", "3", intPtr(3000))}},
		{"mvnm then grouping supply the series",
			map[string]string{"mvnm": "Discworld", "grouping": "Other", "mvin": "1/41"}, flatPath,
			[]HydrationRelation{series("Discworld", "1", intPtr(1000))}},
		{"grouping is the last series tag",
			map[string]string{"grouping": "The Expanse"}, flatPath,
			[]HydrationRelation{series("The Expanse", "", nil)}},
		{"fractional sequence sorts at 4500",
			map[string]string{"series": "The Stormlight Archive", "series-part": "4.50"}, flatPath,
			[]HydrationRelation{series("The Stormlight Archive", "4.5", intPtr(4500))}},
		{"non-numeric sequence kept without sort order",
			map[string]string{"series": "Wheel of Time", "series-part": "Prequel"}, flatPath,
			[]HydrationRelation{series("Wheel of Time", "Prequel", nil)}},
		{"folder supplies series and sequence",
			nil, "/lib/Brandon Sanderson/Mistborn 03/The Hero of Ages/01.mp3",
			[]HydrationRelation{series("Mistborn", "3", intPtr(3000))}},
		{"folder sequence fills a series tag without one",
			map[string]string{"series": "The Stormlight Archive"}, "/lib/Brandon Sanderson/Stormlight Book 4.5/Dawnshard/01.mp3",
			[]HydrationRelation{series("The Stormlight Archive", "4.5", intPtr(4500))}},
		{"bare-number folder names no series",
			nil, "/lib/Author/03/Title/01.mp3", nil},
		{"genre split on semicolon and slash",
			map[string]string{"genre": "Fantasy; Epic / fantasy"}, flatPath,
			[]HydrationRelation{genre("Fantasy"), genre("Epic")}},
		{"empty tags map nothing",
			map[string]string{"artist": "  ", "genre": ""}, "/lib/Author/Title/Book.m4b", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MapHydration(tc.tags, tc.path)
			if !reflect.DeepEqual(got.Relations, tc.want) {
				t.Fatalf("MapHydration relations =\n  %+v\nwant\n  %+v", got.Relations, tc.want)
			}
			if got.Mapped != (len(tc.want) > 0) {
				t.Fatalf("Mapped = %v with %d relations", got.Mapped, len(tc.want))
			}
		})
	}
}

func TestMapHydrationOrdersCreditsSeriesGenres(t *testing.T) {
	got := MapHydration(map[string]string{
		"genre": "Science Fiction", "series": "Dune", "series-part": "1", "artist": "Frank Herbert",
	}, flatPath)
	want := []HydrationRelation{
		person("Frank Herbert", RoleAuthor, 0),
		series("Dune", "1", intPtr(1000)),
		genre("Science Fiction"),
	}
	if !reflect.DeepEqual(got.Relations, want) || !got.Mapped {
		t.Fatalf("MapHydration = %+v, want %+v", got, want)
	}
}

func TestNormaliseSequence(t *testing.T) {
	cases := []struct {
		in        string
		want      string
		wantOrder *int
	}{
		{"0", "0", intPtr(0)},
		{"00.5", "0.5", intPtr(500)},
		{"2.0", "2", intPtr(2000)},
		{"12.345", "12.345", intPtr(12345)},
		{"99999999999", "99999999999", nil},
		{"IV", "IV", nil},
	}
	for _, tc := range cases {
		got, order := normaliseSequence(tc.in)
		if got != tc.want || !reflect.DeepEqual(order, tc.wantOrder) {
			t.Errorf("normaliseSequence(%q) = %q, %v; want %q, %v", tc.in, got, order, tc.want, tc.wantOrder)
		}
	}
}

func TestHydratedFieldDeclared(t *testing.T) {
	for _, field := range Bundle().MediaFields {
		if field.Name == HydratedField {
			if field.Type != "flag" {
				t.Fatalf("%s is declared as %+v, want a flag", HydratedField, field)
			}
			return
		}
	}
	t.Fatalf("bundle declares no %s Media field", HydratedField)
}

// TestHydrationVocabularyDeclared pins every name MapHydration can emit to the
// bundle, so a plan never carries vocabulary core would refuse.
func TestHydrationVocabularyDeclared(t *testing.T) {
	declared := map[string]map[string][]string{}
	for _, kind := range Bundle().EntityKinds {
		declared[kind.Name] = map[string][]string{}
		for _, relation := range kind.Relations {
			declared[kind.Name][relation.Name] = relation.Attributes
		}
	}
	for _, want := range []struct {
		kind, relation, attribute string
	}{
		{KindPerson, RelationCreditedOn, AttributeRole},
		{KindSeries, RelationContainsBook, AttributeSequence},
		{KindGenre, RelationGenreOf, ""},
	} {
		attributes, ok := declared[want.kind][want.relation]
		if !ok {
			t.Fatalf("bundle does not declare %s —%s→", want.kind, want.relation)
		}
		if want.attribute != "" && !reflect.DeepEqual(attributes, []string{want.attribute}) {
			t.Fatalf("%s —%s→ attributes = %v, want [%s]", want.kind, want.relation, attributes, want.attribute)
		}
	}
}

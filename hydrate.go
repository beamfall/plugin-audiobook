package audiobook

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// HydratedField is the declared flag Media field Core's hydration seam sets
// once it has written MapHydration's relations for a Media, so hydration runs
// once per Media; re-tagging a file does not re-hydrate it.
const HydratedField = "audiobook-hydrated"

// Vocabulary declared in bundle.go. MapHydration emits only these names, so a
// hydration plan never invents a Kind, relation or attribute.
const (
	KindPerson = "person"
	KindSeries = "series"
	KindGenre  = "genre"

	RelationCreditedOn   = "credited-on"
	RelationContainsBook = "contains-book"
	RelationGenreOf      = "genre-of"

	AttributeRole     = "role"
	AttributeSequence = "sequence"

	RoleAuthor     = "author"
	RoleNarrator   = "narrator"
	RoleTranslator = "translator"
)

// HydrationEntity names the Entity a hydration relation starts from. Core
// resolves it by Kind and title, creating it when absent.
type HydrationEntity struct {
	Kind  string
	Title string
}

// HydrationRelation is one relation from an Entity to the Media being
// hydrated. Attributes carries the relation's declared string attributes.
// SortOrder is nil when the source gives no order.
type HydrationRelation struct {
	From       HydrationEntity
	Name       string
	Attributes map[string]string
	SortOrder  *int
}

// Hydration is the plan MapHydration derives for one audiobook Media. Mapped
// reports whether it holds any relation.
type Hydration struct {
	Relations []HydrationRelation
	Mapped    bool
}

// maxSequenceSortValue bounds a sequence before it is scaled to a sort order,
// so an absurd tag cannot overflow the integer.
const maxSequenceSortValue = 1e9

var (
	// Multi-value separators for people and genres. Commas are not split: a
	// "Last, First" name would be torn in two.
	personSeparators = regexp.MustCompile(`\s*(?:;|/|\s&\s|\sand\s)\s*`)
	genreSeparators  = regexp.MustCompile(`\s*[;/]\s*`)
	reSequence       = regexp.MustCompile(`^(\d+)(?:\.(\d+))?$`)
)

// MapHydration maps one audiobook file's embedded tags (keys matched
// case-insensitively) and its path to Person credits, Series membership and
// Genre links on the Media. Tags win; the "Author/Series ##/Title/" folder
// supplies the series and sequence only when no tag does. The author folder is
// never used: without the library root, "Library/Title/" and "Author/Title/"
// look the same.
//
//   - author: album_artist, else artist, else author
//   - narrator: narrator, else composer
//   - translator: translator
//   - series: series, else mvnm, else grouping; sequence: series-part, else
//     mvin, else the series folder's number
//   - genre: genre
//
// People are split on ";", "/", " & " and " and "; genres on ";" and "/". A
// person in several roles gets one credit whose role lists them in the order
// author, narrator, translator ("author, narrator"). Credits are ordered by
// first appearance, authors first, and carry that position as SortOrder.
func MapHydration(tags map[string]string, path string) Hydration {
	lookup := lowerKeys(tags)
	var relations []HydrationRelation
	relations = append(relations, credits(lookup)...)
	if series, ok := seriesRelation(lookup, path); ok {
		relations = append(relations, series)
	}
	for _, genre := range splitValues(firstTag(lookup, "genre"), genreSeparators) {
		relations = append(relations, HydrationRelation{
			From: HydrationEntity{Kind: KindGenre, Title: genre},
			Name: RelationGenreOf,
		})
	}
	return Hydration{Relations: relations, Mapped: len(relations) > 0}
}

// lowerKeys copies tags under lowercase keys. Keys are visited in sorted order
// so the result is deterministic; an already-lowercase key wins a collision.
func lowerKeys(tags map[string]string) map[string]string {
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lower := make(map[string]string, len(tags))
	for _, key := range keys {
		value := strings.TrimSpace(tags[key])
		if value == "" {
			continue
		}
		lk := strings.ToLower(key)
		if _, seen := lower[lk]; !seen || lk == key {
			lower[lk] = value
		}
	}
	return lower
}

// firstTag returns the first non-empty tag among keys, which must be lowercase.
func firstTag(tags map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := tags[key]; value != "" {
			return value
		}
	}
	return ""
}

// splitValues splits a multi-value tag, collapses whitespace, drops empties
// and removes case-insensitive duplicates, keeping first appearance.
func splitValues(raw string, separators *regexp.Regexp) []string {
	if raw == "" {
		return nil
	}
	var values []string
	seen := map[string]bool{}
	for _, part := range separators.Split(raw, -1) {
		value := strings.Join(strings.Fields(part), " ")
		key := strings.ToLower(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		values = append(values, value)
	}
	return values
}

type credit struct {
	name  string
	roles []string
}

func credits(tags map[string]string) []HydrationRelation {
	sources := []struct {
		role string
		raw  string
	}{
		{RoleAuthor, firstTag(tags, "album_artist", "artist", "author")},
		{RoleNarrator, firstTag(tags, "narrator", "composer")},
		{RoleTranslator, firstTag(tags, "translator")},
	}
	var people []*credit
	byName := map[string]*credit{}
	for _, source := range sources {
		for _, name := range splitValues(source.raw, personSeparators) {
			key := strings.ToLower(name)
			person := byName[key]
			if person == nil {
				person = &credit{name: name}
				byName[key] = person
				people = append(people, person)
			}
			person.roles = append(person.roles, source.role)
		}
	}
	relations := make([]HydrationRelation, 0, len(people))
	for i, person := range people {
		relations = append(relations, HydrationRelation{
			From:       HydrationEntity{Kind: KindPerson, Title: person.name},
			Name:       RelationCreditedOn,
			Attributes: map[string]string{AttributeRole: strings.Join(person.roles, ", ")},
			SortOrder:  intPtr(i),
		})
	}
	return relations
}

func seriesRelation(tags map[string]string, path string) (HydrationRelation, bool) {
	folderName, folderSequence := seriesFromFolder(path)
	name := firstNonEmpty(firstTag(tags, "series", "mvnm", "grouping"), folderName)
	if name == "" {
		return HydrationRelation{}, false
	}
	relation := HydrationRelation{
		From: HydrationEntity{Kind: KindSeries, Title: strings.Join(strings.Fields(name), " ")},
		Name: RelationContainsBook,
	}
	rawSequence, _ := trackNumber(firstTag(tags, "series-part", "mvin"))
	sequence, sortOrder := normaliseSequence(firstNonEmpty(rawSequence, folderSequence))
	if sequence != "" {
		relation.Attributes = map[string]string{AttributeSequence: sequence}
		relation.SortOrder = sortOrder
	}
	return relation, true
}

// normaliseSequence trims leading zeros from the whole part and trailing zeros
// from the fraction ("04" → "4", "4.50" → "4.5", "2.0" → "2") and returns the
// sort order round(sequence × 1000). A value that is not a plain decimal
// number is kept as written, with no sort order.
func normaliseSequence(raw string) (string, *int) {
	raw = strings.TrimSpace(raw)
	m := reSequence.FindStringSubmatch(raw)
	if m == nil {
		return raw, nil
	}
	sequence := strings.TrimLeft(m[1], "0")
	if sequence == "" {
		sequence = "0"
	}
	if fraction := strings.TrimRight(m[2], "0"); fraction != "" {
		sequence += "." + fraction
	}
	value, err := strconv.ParseFloat(sequence, 64)
	if err != nil || value > maxSequenceSortValue {
		return sequence, nil
	}
	return sequence, intPtr(int(math.Round(value * 1000)))
}

func intPtr(value int) *int {
	return &value
}

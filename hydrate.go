package audiobook

import (
	"html"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
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
// so an absurd tag cannot overflow the integer, even a 32-bit one.
const maxSequenceSortValue = 1e6

var (
	// Multi-value separators for genres.
	genreSeparators      = regexp.MustCompile(`\s*[;/,]\s*`)
	reSequence           = regexp.MustCompile(`^(\d+)(?:\.(\d+))?$`)
	reTotal              = regexp.MustCompile(`^\d+$`)
	reBracketedRole      = regexp.MustCompile(`^(.*?)\s*[\(\[]\s*([^\)\]]+?)\s*[\)\]]\s*$`)
	reSeparatedRole      = regexp.MustCompile(`^(.*?)\s*(?:[-–—]+|,)\s*([A-Za-z][A-Za-z ]+)$`)
	reLeadingRolePrefix  = regexp.MustCompile(`(?i)^(?:read\s+by|narrated\s+by|translated\s+by|edited\s+by|abridged\s+by)\s+(.+)$`)
	reLeadingRoleKeyword = regexp.MustCompile(`(?i)^(read\s+by|narrated\s+by|translated\s+by|edited\s+by|abridged\s+by)\s+`)
)

var nonPersonPlaceholders = map[string]bool{
	"full cast":          true,
	"full cast audio":    true,
	"cast":               true,
	"various":            true,
	"various authors":    true,
	"various narrators":  true,
	"multiple narrators": true,
	"unknown":            true,
	"unknown author":     true,
	"unknown artist":     true,
	"uncredited":         true,
	"n/a":                true,
	"none":               true,
}

var genreStopwords = map[string]bool{
	"audiobook":  true,
	"audiobooks": true,
	"hörbuch":    true,
	"other":      true,
	"unknown":    true,
}

var commaJoinSuffixes = map[string]bool{
	"jr": true, "jr.": true, "sr": true, "sr.": true,
	"ii": true, "iii": true, "iv": true,
	"phd": true, "ph.d.": true, "md": true, "m.d.": true,
	"esq": true, "esq.": true,
}

var roleKeywords = map[string]string{
	"author":        RoleAuthor,
	"narrator":      RoleNarrator,
	"reader":        RoleNarrator,
	"read by":       RoleNarrator,
	"narrated by":   RoleNarrator,
	"translator":    RoleTranslator,
	"translated by": RoleTranslator,
	"foreword":      "foreword",
	"introduction":  "introduction",
	"intro":         "introduction",
	"afterword":     "afterword",
	"preface":       "preface",
	"editor":        "editor",
	"edited by":     "editor",
	"illustrator":   "illustrator",
	"contributor":   "contributor",
	"commentary":    "commentary",
	"abridged by":   "abridged",
	"adapter":       "adapter",
}

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
	for _, genre := range splitGenres(firstTag(lookup, "genre")) {
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

// splitGenres decodes HTML entities, splits on ";", "/", and ",", collapses
// whitespace, drops stopwords/empties and deduplicates case-insensitively.
func splitGenres(raw string) []string {
	if raw == "" {
		return nil
	}
	decoded := html.UnescapeString(raw)
	var values []string
	seen := map[string]bool{}
	for _, part := range genreSeparators.Split(decoded, -1) {
		value := strings.Join(strings.Fields(part), " ")
		key := foldKey(value)
		if value == "" || genreStopwords[strings.ToLower(value)] || seen[key] {
			continue
		}
		seen[key] = true
		values = append(values, value)
	}
	return values
}

type personEntry struct {
	name string
	role string
}

var personNonSlashSeparators = regexp.MustCompile(`\s*(?:;|\s&\s|\sand\s)\s*`)

func parsePeople(raw, defaultRole string) []personEntry {
	if raw == "" {
		return nil
	}
	decoded := strings.Join(strings.Fields(html.UnescapeString(raw)), " ")
	if decoded == "" || nonPersonPlaceholders[strings.ToLower(decoded)] {
		return nil
	}
	var entries []personEntry
	for _, chunk := range personNonSlashSeparators.Split(decoded, -1) {
		for _, commaPart := range splitPersonCommas(chunk) {
			trimmed := strings.Join(strings.Fields(commaPart), " ")
			if trimmed == "" || nonPersonPlaceholders[strings.ToLower(trimmed)] {
				continue
			}
			for _, candidate := range strings.Split(trimmed, "/") {
				name, role := cleanPersonRole(candidate, defaultRole)
				if name == "" || nonPersonPlaceholders[strings.ToLower(name)] {
					continue
				}
				entries = append(entries, personEntry{name: name, role: role})
			}
		}
	}
	return entries
}

func splitPersonCommas(chunk string) []string {
	rawParts := strings.Split(chunk, ",")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		cleaned := strings.Join(strings.Fields(part), " ")
		if cleaned == "" {
			continue
		}
		if len(parts) > 0 {
			lower := strings.ToLower(cleaned)
			if _, isRole := roleKeywords[lower]; isRole || commaJoinSuffixes[lower] {
				parts[len(parts)-1] = parts[len(parts)-1] + ", " + cleaned
				continue
			}
		}
		parts = append(parts, cleaned)
	}
	if len(parts) == 2 && !nonPersonPlaceholders[strings.ToLower(parts[0])] && !nonPersonPlaceholders[strings.ToLower(parts[1])] {
		if len(strings.Fields(parts[0])) == 1 || len(strings.Fields(parts[1])) == 1 {
			return []string{parts[0] + ", " + parts[1]}
		}
	}
	return parts
}

func cleanPersonRole(raw, defaultRole string) (string, string) {
	name := strings.Join(strings.Fields(raw), " ")
	role := defaultRole
	if m := reLeadingRoleKeyword.FindStringSubmatch(name); m != nil {
		if mapped, ok := roleKeywords[strings.ToLower(strings.Join(strings.Fields(m[1]), " "))]; ok {
			role = mapped
			if rest := reLeadingRolePrefix.FindStringSubmatch(name); rest != nil {
				name = strings.TrimSpace(rest[1])
			}
		}
	}
	for {
		if m := reBracketedRole.FindStringSubmatch(name); m != nil {
			if mapped, ok := roleKeywords[strings.ToLower(strings.Join(strings.Fields(m[2]), " "))]; ok {
				role = mapped
				name = strings.TrimSpace(m[1])
				continue
			}
		}
		if m := reSeparatedRole.FindStringSubmatch(name); m != nil {
			if mapped, ok := roleKeywords[strings.ToLower(strings.Join(strings.Fields(m[2]), " "))]; ok {
				role = mapped
				name = strings.TrimSpace(m[1])
				continue
			}
		}
		break
	}
	name = strings.Join(strings.Fields(name), " ")
	if _, isRoleOnly := roleKeywords[strings.ToLower(name)]; isRoleOnly {
		return "", role
	}
	return name, role
}

func personKeySet(entries []personEntry) map[string]bool {
	set := make(map[string]bool, len(entries))
	for _, e := range entries {
		set[foldKey(e.name)] = true
	}
	return set
}

func allPeopleInSet(entries []personEntry, set map[string]bool) bool {
	if len(entries) == 0 {
		return false
	}
	for _, e := range entries {
		if !set[foldKey(e.name)] {
			return false
		}
	}
	return true
}

func hasPersonOutsideSet(entries []personEntry, set map[string]bool) bool {
	for _, e := range entries {
		if !set[foldKey(e.name)] {
			return true
		}
	}
	return false
}

type credit struct {
	name  string
	roles []string
}

func credits(tags map[string]string) []HydrationRelation {
	composerEntries := parsePeople(tags["composer"], RoleNarrator)
	composerKeys := personKeySet(composerEntries)

	authorEntries := parsePeople(tags["album_artist"], RoleAuthor)
	if len(authorEntries) == 0 {
		artistEntries := parsePeople(tags["artist"], RoleAuthor)
		tagAuthorEntries := parsePeople(tags["author"], RoleAuthor)
		if len(artistEntries) > 0 && len(tagAuthorEntries) > 0 && allPeopleInSet(artistEntries, composerKeys) {
			authorEntries = tagAuthorEntries
		} else if len(artistEntries) > 0 {
			authorEntries = artistEntries
		} else {
			authorEntries = tagAuthorEntries
		}
	}
	if len(composerKeys) > 0 && hasPersonOutsideSet(authorEntries, composerKeys) {
		filtered := authorEntries[:0]
		for _, entry := range authorEntries {
			if entry.role == RoleAuthor && composerKeys[foldKey(entry.name)] {
				continue
			}
			filtered = append(filtered, entry)
		}
		authorEntries = filtered
	}

	narratorEntries := parsePeople(tags["narrator"], RoleNarrator)
	if len(narratorEntries) == 0 {
		narratorEntries = composerEntries
	}
	translatorEntries := parsePeople(tags["translator"], RoleTranslator)

	var people []*credit
	byName := map[string]*credit{}
	for _, list := range [][]personEntry{authorEntries, narratorEntries, translatorEntries} {
		for _, entry := range list {
			key := foldKey(entry.name)
			person := byName[key]
			if person == nil {
				person = &credit{name: entry.name}
				byName[key] = person
				people = append(people, person)
			}
			if !slices.Contains(person.roles, entry.role) {
				person.roles = append(person.roles, entry.role)
			}
		}
	}
	relations := make([]HydrationRelation, 0, len(people))
	for i, person := range people {
		sortRoles(person.roles)
		relations = append(relations, HydrationRelation{
			From:       HydrationEntity{Kind: KindPerson, Title: person.name},
			Name:       RelationCreditedOn,
			Attributes: map[string]string{AttributeRole: strings.Join(person.roles, ", ")},
			SortOrder:  intPtr(i),
		})
	}
	return relations
}

func sortRoles(roles []string) {
	rank := func(role string) int {
		switch role {
		case RoleAuthor:
			return 0
		case RoleNarrator:
			return 1
		case RoleTranslator:
			return 2
		default:
			return 3
		}
	}
	slices.SortStableFunc(roles, func(a, b string) int {
		return rank(a) - rank(b)
	})
}

func seriesRelation(tags map[string]string, path string) (HydrationRelation, bool) {
	folderName, folderSequence := seriesFromFolder(path)
	name := html.UnescapeString(firstNonEmpty(firstTag(tags, "series", "mvnm", "grouping"), folderName))
	if name == "" {
		return HydrationRelation{}, false
	}
	relation := HydrationRelation{
		From: HydrationEntity{Kind: KindSeries, Title: strings.Join(strings.Fields(name), " ")},
		Name: RelationContainsBook,
	}
	sequence, sortOrder := normaliseSequence(firstNonEmpty(sequenceTag(tags), folderSequence))
	if sequence != "" {
		relation.Attributes = map[string]string{AttributeSequence: sequence}
		relation.SortOrder = sortOrder
	}
	return relation, true
}

// sequenceTag returns series-part, else mvin, dropping a "/total" suffix only
// from a numeric "N/total" value so a text sequence keeps its slash.
func sequenceTag(tags map[string]string) string {
	raw := firstTag(tags, "series-part", "mvin")
	number, total := trackNumber(raw)
	if reSequence.MatchString(number) && (total == "" || reTotal.MatchString(total)) {
		return number
	}
	return raw
}

// foldKey is a case-insensitive comparison key under Unicode simple case
// folding: each rune maps to the smallest rune of its fold orbit, so Greek
// final sigma matches sigma while Turkish dotless i stays distinct from i.
func foldKey(value string) string {
	return strings.Map(func(r rune) rune {
		smallest := r
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			if f < smallest {
				smallest = f
			}
		}
		return smallest
	}, value)
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

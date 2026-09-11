package metadata

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxNFOBytes    = 2 * 1024 * 1024
	maxXMLDepth    = 64
	maxElements    = 16_384
	maxAttributes  = 64
	maxFieldBytes  = 64 * 1024
	maxNameBytes   = 1024
	maxEntries     = 1024
	maxProviderKey = 64
	maxProviderID  = 256
)

// Metadata contains local descriptive values without filesystem or network data.
type Metadata struct {
	Kind, Name, SortName, OriginalTitle, Overview, OfficialRating string
	ProductionYear                                                *int
	PremiereDate                                                  *time.Time
	IndexNumber, ParentIndexNumber                                *int
	CommunityRating                                               *float64
	ProviderIDs                                                   map[string]string
	Genres, Tags, Studios                                         []string
	People                                                        []Person
	Album                                                         string   `json:"Album,omitempty"`
	Artists                                                       []string `json:"Artists,omitempty"`
	AlbumArtists                                                  []string `json:"AlbumArtists,omitempty"`
}

// Person is a local credit. SortOrder preserves explicit zero-based ordering.
type Person struct {
	Name, Role, Type string
	SortOrder        *int
}

type element struct {
	name     xml.Name
	attrs    []xml.Attr
	text     strings.Builder
	children []*element
}

// ParseNFO reads at most 2 MiB plus one overflow byte from r. It does not close r.
// All limits and field errors fail the entire document without partial output.
func ParseNFO(r io.Reader) (Metadata, error) {
	if r == nil {
		return Metadata{}, fmt.Errorf("NFO reader is nil")
	}
	data, err := io.ReadAll(io.LimitReader(r, maxNFOBytes+1))
	if err != nil {
		return Metadata{}, fmt.Errorf("read NFO: %w", err)
	}
	if len(data) > maxNFOBytes {
		return Metadata{}, fmt.Errorf("NFO input exceeds %d bytes", maxNFOBytes)
	}
	if !utf8.Valid(data) {
		return Metadata{}, fmt.Errorf("NFO input must be valid UTF-8")
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	root, err := parseXML(data)
	if err != nil {
		return Metadata{}, err
	}
	result, err := extract(root)
	if err != nil {
		return Metadata{}, fmt.Errorf("NFO metadata: %w", err)
	}
	return result, nil
}

func parseXML(data []byte) (*element, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	// Keep Strict enabled and Entity/CharsetReader unset: only built-in XML
	// entities and UTF-8 input are supported, with no external resolver.
	var root *element
	var stack []*element
	var elements int
	var contentSeen bool
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode NFO XML: %w", err)
		}
		switch token := token.(type) {
		case xml.StartElement:
			contentSeen = true
			elements++
			if elements > maxElements || len(stack) >= maxXMLDepth {
				return nil, fmt.Errorf("NFO XML structure exceeds element or depth limit")
			}
			if err := checkAttributes(token); err != nil {
				return nil, err
			}
			node := &element{name: token.Name, attrs: token.Attr}
			if len(stack) == 0 {
				if root != nil {
					return nil, fmt.Errorf("NFO XML must contain exactly one root")
				}
				root = node
			} else {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, node)
			}
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fmt.Errorf("unexpected NFO XML closing element")
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				if !xmlWhitespace(token) {
					return nil, fmt.Errorf("NFO XML has text outside its root")
				}
			} else {
				current := stack[len(stack)-1]
				if len(token) > maxFieldBytes-current.text.Len() {
					return nil, fmt.Errorf("NFO XML text exceeds field limit")
				}
				current.text.Write(token)
			}
			contentSeen = true
		case xml.Directive:
			return nil, fmt.Errorf("NFO XML directives and DOCTYPE are not supported")
		case xml.ProcInst:
			if token.Target != "xml" || contentSeen || root != nil {
				return nil, fmt.Errorf("NFO XML processing instruction is not supported here")
			}
			if err := checkDeclaration(token.Inst); err != nil {
				return nil, err
			}
			contentSeen = true
		case xml.Comment:
			contentSeen = true
		}
	}
	if root == nil || len(stack) != 0 {
		return nil, fmt.Errorf("NFO XML has no complete root element")
	}
	return root, nil
}

func xmlWhitespace(data []byte) bool {
	for _, character := range data {
		if character != ' ' && character != '\t' && character != '\r' && character != '\n' {
			return false
		}
	}
	return true
}

func checkAttributes(token xml.StartElement) error {
	if len(token.Name.Local) > maxFieldBytes || len(token.Name.Space) > maxFieldBytes || len(token.Attr) > maxAttributes {
		return fmt.Errorf("NFO XML name or attributes exceed limit")
	}
	seen := make(map[xml.Name]bool, len(token.Attr))
	for _, attribute := range token.Attr {
		if seen[attribute.Name] {
			return fmt.Errorf("NFO XML has duplicate attributes")
		}
		seen[attribute.Name] = true
		if len(attribute.Name.Local) > maxFieldBytes || len(attribute.Name.Space) > maxFieldBytes || len(attribute.Value) > maxFieldBytes {
			return fmt.Errorf("NFO XML attribute exceeds field limit")
		}
	}
	return nil
}

// Let encoding/xml parse declaration attributes instead of introducing a second
// XML lexer. Declaration syntax is restricted to XML 1.0 and UTF-8.
func checkDeclaration(data []byte) error {
	if len(data) > maxFieldBytes || bytes.ContainsAny(data, "<>&") {
		return fmt.Errorf("invalid NFO XML declaration")
	}
	decoder := xml.NewDecoder(strings.NewReader("<declaration " + string(data) + "/>"))
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("invalid NFO XML declaration: %w", err)
	}
	start, ok := token.(xml.StartElement)
	if !ok || len(start.Attr) == 0 || start.Attr[0].Name != (xml.Name{Local: "version"}) || start.Attr[0].Value != "1.0" {
		return fmt.Errorf("NFO XML declaration must begin with version 1.0")
	}
	if err := checkAttributes(start); err != nil {
		return err
	}
	standaloneSeen := false
	for _, attribute := range start.Attr[1:] {
		if attribute.Name.Space != "" {
			return fmt.Errorf("invalid NFO XML declaration attribute")
		}
		switch attribute.Name.Local {
		case "encoding":
			if standaloneSeen || !strings.EqualFold(attribute.Value, "UTF-8") {
				return fmt.Errorf("NFO XML declaration requires UTF-8 before standalone")
			}
		case "standalone":
			if attribute.Value != "yes" && attribute.Value != "no" {
				return fmt.Errorf("invalid NFO XML standalone declaration")
			}
			standaloneSeen = true
		default:
			return fmt.Errorf("unsupported NFO XML declaration attribute")
		}
	}
	if token, err = decoder.Token(); err != nil {
		return fmt.Errorf("invalid NFO XML declaration: %w", err)
	}
	if _, ok := token.(xml.EndElement); !ok {
		return fmt.Errorf("invalid NFO XML declaration content")
	}
	if _, err = decoder.Token(); err != io.EOF {
		return fmt.Errorf("invalid trailing NFO XML declaration content")
	}
	return nil
}

func extract(root *element) (Metadata, error) {
	result := Metadata{Kind: strings.ToLower(root.name.Local)}
	if root.name.Space != "" {
		return Metadata{}, fmt.Errorf("namespaced NFO roots are not supported")
	}
	switch result.Kind {
	case "movie", "tvshow", "episodedetails", "season", "album", "artist":
	default:
		return Metadata{}, fmt.Errorf("unsupported NFO root")
	}
	if !xmlWhitespace([]byte(root.text.String())) {
		return Metadata{}, fmt.Errorf("NFO root must contain metadata elements")
	}
	fields := make(map[string]string)
	for _, child := range root.children {
		if child.name.Space != "" {
			continue
		}
		name := strings.ToLower(child.name.Local)
		switch name {
		case "title", "sorttitle", "originaltitle", "plot", "year", "premiered", "aired", "season", "episode", "rating", "mpaa":
			if err := collectScalar(fields, name, child); err != nil {
				return Metadata{}, err
			}
		case "name":
			if result.Kind == "album" || result.Kind == "artist" {
				if err := collectScalar(fields, name, child); err != nil {
					return Metadata{}, err
				}
			}
		case "genre", "tag", "studio":
			value, err := scalarText(child)
			if err != nil {
				return Metadata{}, err
			}
			target := &result.Genres
			if name == "tag" {
				target = &result.Tags
			} else if name == "studio" {
				target = &result.Studios
			}
			if err := appendUnique(target, value); err != nil {
				return Metadata{}, err
			}
		case "uniqueid", "imdbid", "tmdbid", "tvdbid":
			if err := collectProvider(&result, child, name); err != nil {
				return Metadata{}, err
			}
		case "actor", "director", "writer", "credits":
			person, err := parsePerson(child, name)
			if err != nil {
				return Metadata{}, err
			}
			if person.Name != "" {
				if len(result.People) >= maxEntries {
					return Metadata{}, fmt.Errorf("NFO people exceed collection limit")
				}
				result.People = append(result.People, person)
			}
		}
	}
	result.Name = fields["title"]
	if result.Name == "" {
		result.Name = fields["name"]
	}
	result.SortName = fields["sorttitle"]
	// Catalog names participate in PostgreSQL B-tree indexes. Bound their
	// decoded UTF-8 bytes before a valid NFO can make an entire scan fail.
	if len(result.Name) > maxNameBytes || len(result.SortName) > maxNameBytes {
		return Metadata{}, fmt.Errorf("NFO title and sort title must not exceed %d UTF-8 bytes", maxNameBytes)
	}
	result.OriginalTitle = fields["originaltitle"]
	result.Overview = fields["plot"]
	result.OfficialRating = fields["mpaa"]
	var err error
	if result.ProductionYear, err = parseInteger(fields["year"], "year", 1, 9999); err != nil {
		return Metadata{}, err
	}
	season, err := parseInteger(fields["season"], "season", 0, math.MaxInt32)
	if err != nil {
		return Metadata{}, err
	}
	episode, err := parseInteger(fields["episode"], "episode", 0, math.MaxInt32)
	if err != nil {
		return Metadata{}, err
	}
	if result.Kind == "season" {
		result.IndexNumber = season
	} else if result.Kind == "episodedetails" {
		result.IndexNumber, result.ParentIndexNumber = episode, season
	}
	if result.CommunityRating, err = parseRating(fields["rating"]); err != nil {
		return Metadata{}, err
	}
	premiered, err := parseDate(fields["premiered"], "premiered")
	if err != nil {
		return Metadata{}, err
	}
	aired, err := parseDate(fields["aired"], "aired")
	if err != nil {
		return Metadata{}, err
	}
	result.PremiereDate = premiered
	if premiered == nil {
		result.PremiereDate = aired
	}
	return result, nil
}

func scalarText(node *element) (string, error) {
	if len(node.children) != 0 {
		return "", fmt.Errorf("NFO scalar fields must contain text only")
	}
	return strings.TrimSpace(node.text.String()), nil
}

func collectScalar(fields map[string]string, name string, node *element) error {
	value, err := scalarText(node)
	if err != nil || value == "" {
		return err
	}
	if previous, exists := fields[name]; exists && previous != value {
		return fmt.Errorf("NFO field %s has conflicting values", name)
	}
	fields[name] = value
	return nil
}

func appendUnique(target *[]string, value string) error {
	if value == "" {
		return nil
	}
	for _, existing := range *target {
		if existing == value {
			return nil
		}
	}
	if len(*target) >= maxEntries {
		return fmt.Errorf("NFO values exceed collection limit")
	}
	*target = append(*target, value)
	return nil
}

func collectProvider(result *Metadata, node *element, name string) error {
	value, err := scalarText(node)
	if err != nil || value == "" {
		return err
	}
	key := strings.TrimSuffix(name, "id")
	if name == "uniqueid" {
		key = ""
		for _, attribute := range node.attrs {
			if attribute.Name.Space == "" && attribute.Name.Local == "type" {
				key = strings.TrimSpace(attribute.Value)
			}
		}
	}
	if len(key) == 0 || len(key) > maxProviderKey || !safeASCII(key, "") {
		return fmt.Errorf("NFO provider key must be bounded ASCII alphanumeric text")
	}
	switch strings.ToLower(key) {
	case "imdb":
		key = "Imdb"
	case "tmdb":
		key = "Tmdb"
	case "tvdb":
		key = "Tvdb"
	}
	if len(value) > maxProviderID || !safeASCII(value, "._:-") {
		return fmt.Errorf("NFO provider ID contains unsupported characters or exceeds limit")
	}
	if previous, exists := result.ProviderIDs[key]; exists {
		if previous != value {
			return fmt.Errorf("NFO provider %s has conflicting IDs", key)
		}
		return nil
	}
	if len(result.ProviderIDs) >= maxEntries {
		return fmt.Errorf("NFO providers exceed collection limit")
	}
	if result.ProviderIDs == nil {
		result.ProviderIDs = make(map[string]string)
	}
	result.ProviderIDs[key] = value
	return nil
}

func safeASCII(value, extra string) bool {
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || strings.ContainsRune(extra, character) {
			continue
		}
		return false
	}
	return true
}

func parsePerson(node *element, name string) (Person, error) {
	person := Person{Type: "Writer"}
	if name == "director" {
		person.Type = "Director"
	} else if name == "actor" {
		person.Type = "Actor"
	}
	if name != "actor" {
		value, err := scalarText(node)
		person.Name = value
		return person, err
	}
	if !xmlWhitespace([]byte(node.text.String())) {
		return Person{}, fmt.Errorf("NFO actor must contain credit fields")
	}
	fields := make(map[string]string)
	for _, child := range node.children {
		if child.name.Space != "" {
			continue
		}
		field := strings.ToLower(child.name.Local)
		if field == "name" || field == "role" || field == "order" {
			if err := collectScalar(fields, field, child); err != nil {
				return Person{}, err
			}
		}
	}
	person.Name, person.Role = fields["name"], fields["role"]
	var err error
	person.SortOrder, err = parseInteger(fields["order"], "actor order", 0, math.MaxInt32)
	if err != nil {
		return Person{}, err
	}
	if person.Name == "" && (person.Role != "" || person.SortOrder != nil) {
		return Person{}, fmt.Errorf("NFO actor credit requires a name")
	}
	return person, nil
}

func parseInteger(text, field string, minimum, maximum int64) (*int, error) {
	if text == "" {
		return nil, nil
	}
	for _, character := range text {
		if character < '0' || character > '9' {
			return nil, fmt.Errorf("NFO %s must be a decimal integer", field)
		}
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil || value < minimum || value > maximum {
		return nil, fmt.Errorf("NFO %s is outside its allowed range", field)
	}
	result := int(value)
	return &result, nil
}

func parseRating(text string) (*float64, error) {
	if text == "" {
		return nil, nil
	}
	// Hexadecimal, nonfinite, and signed forms are outside the local contract.
	for _, character := range text {
		if character < '0' || character > '9' {
			if character != '.' {
				return nil, fmt.Errorf("NFO rating must be a decimal number from 0 to 10")
			}
		}
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 10 {
		return nil, fmt.Errorf("NFO rating must be a decimal number from 0 to 10")
	}
	return &value, nil
}

func parseDate(text, field string) (*time.Time, error) {
	if text == "" {
		return nil, nil
	}
	if !dateSyntax(text) {
		return nil, fmt.Errorf("NFO %s must be a date or RFC3339 timestamp", field)
	}
	value, err := time.Parse("2006-01-02", text)
	if err != nil {
		value, err = time.Parse(time.RFC3339, text)
	}
	if err != nil || value.Year() < 1 || value.Year() > 9999 {
		return nil, fmt.Errorf("NFO %s must be a valid date or RFC3339 timestamp", field)
	}
	value = value.UTC()
	if value.Year() < 1 || value.Year() > 9999 {
		return nil, fmt.Errorf("NFO %s UTC date is outside its allowed range", field)
	}
	return &value, nil
}

// time.Parse intentionally accepts some forms outside RFC3339, including a
// one-digit hour and certain out-of-range timezone offsets. Check the wire
// shape first so malformed input cannot be silently normalized or truncated.
func dateSyntax(text string) bool {
	if len(text) < 10 || text[4] != '-' || text[7] != '-' ||
		!decimalDigits(text[:4]) || !decimalDigits(text[5:7]) || !decimalDigits(text[8:10]) {
		return false
	}
	if len(text) == 10 {
		return true
	}
	if len(text) < 20 || text[10] != 'T' || text[13] != ':' || text[16] != ':' ||
		!decimalDigits(text[11:13]) || !decimalDigits(text[14:16]) || !decimalDigits(text[17:19]) {
		return false
	}
	zone := len(text) - 1
	if text[zone] != 'Z' {
		zone = len(text) - 6
		if zone < 19 || (text[zone] != '+' && text[zone] != '-') || text[zone+3] != ':' ||
			!decimalDigits(text[zone+1:zone+3]) || !decimalDigits(text[zone+4:]) {
			return false
		}
		hour := (text[zone+1]-'0')*10 + text[zone+2] - '0'
		minute := (text[zone+4]-'0')*10 + text[zone+5] - '0'
		if hour > 23 || minute > 59 {
			return false
		}
	}
	if zone == 19 {
		return true
	}
	return zone >= 21 && zone <= 29 && text[19] == '.' && decimalDigits(text[20:zone])
}

func decimalDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, character := range text {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

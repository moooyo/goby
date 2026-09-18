package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/metadata"
)

const musicBrainzBaseURL = "https://musicbrainz.org/ws/2/"

// All clients in this process share a gate because MusicBrainz limits each
// source IP to one request per second, including searches and lookups.
var musicBrainzRequests = struct {
	gate chan struct{}
	next time.Time
}{gate: make(chan struct{}, 1)}

type musicBrainzEntity struct {
	ID               string                    `json:"id"`
	Name             string                    `json:"name"`
	Title            string                    `json:"title"`
	SortName         string                    `json:"sort-name"`
	Disambiguation   string                    `json:"disambiguation"`
	Annotation       string                    `json:"annotation"`
	FirstReleaseDate string                    `json:"first-release-date"`
	Score            json.RawMessage           `json:"score"`
	ArtistCredit     []musicBrainzArtistCredit `json:"artist-credit"`
	Tags             []musicBrainzTag          `json:"tags"`
	Genres           []musicBrainzTag          `json:"genres"`
	Relations        []musicBrainzRelation     `json:"relations"`
}

type musicBrainzArtistCredit struct {
	Name   string `json:"name"`
	Artist struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"artist"`
}

type musicBrainzTag struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type musicBrainzRelation struct {
	Type       string   `json:"type"`
	Attributes []string `json:"attributes"`
	Artist     *struct {
		Name string `json:"name"`
	} `json:"artist"`
	Work *struct {
		Relations []musicBrainzRelation `json:"relations"`
	} `json:"work"`
}

func (client *Client) searchMusicBrainz(ctx context.Context, query Query) ([]Match, error) {
	entity, _, ok := musicBrainzEntityType(query.Type)
	if !ok {
		return nil, ErrInvalidInput
	}
	searchField := entity
	if entity == "release-group" {
		searchField = "releasegroup"
	}
	search := searchField + ":" + musicBrainzSearchPhrase(query.Name)
	if query.Year > 0 && entity != "artist" {
		search += " AND firstreleasedate:" + strconv.Itoa(query.Year)
	}
	parameters := url.Values{"query": {search}, "fmt": {"json"}, "limit": {"25"}}
	var result struct {
		ReleaseGroups []musicBrainzEntity `json:"release-groups"`
		Artists       []musicBrainzEntity `json:"artists"`
		Recordings    []musicBrainzEntity `json:"recordings"`
	}
	if err := client.requestMusicBrainz(ctx, entity+"/?"+parameters.Encode(), &result); err != nil {
		return nil, err
	}
	entities := result.ReleaseGroups
	switch entity {
	case "artist":
		entities = result.Artists
	case "recording":
		entities = result.Recordings
	}
	matches := make([]Match, 0, len(entities))
	seen := make(map[string]bool)
	for _, value := range entities {
		name := musicBrainzName(value, query.Type)
		id := strings.ToLower(value.ID)
		if !musicBrainzID(id) || name == "" || seen[id] {
			continue
		}
		seen[id] = true
		matches = append(matches, Match{
			Selection: Selection{Provider: "musicbrainz", ID: id, Type: query.Type, Language: query.Language},
			Name:      name,
			Year:      musicBrainzYear(value.FirstReleaseDate),
			Overview:  strings.TrimSpace(value.Disambiguation),
			Score:     musicBrainzScore(value.Score),
		})
		if len(matches) == 25 {
			break
		}
	}
	return matches, nil
}

func (client *Client) lookupMusicBrainz(ctx context.Context, selection Selection) (Metadata, error) {
	entity, providerKey, ok := musicBrainzEntityType(selection.Type)
	if !ok || !musicBrainzID(selection.ID) {
		return Metadata{}, ErrInvalidInput
	}
	includes := "tags+genres+annotation+artist-rels"
	if entity != "artist" {
		includes += "+artist-credits"
	}
	if entity == "recording" {
		includes += "+work-rels+work-level-rels"
	}
	parameters := url.Values{"inc": {includes}, "fmt": {"json"}}
	var value musicBrainzEntity
	if err := client.requestMusicBrainz(ctx, entity+"/"+strings.ToLower(selection.ID)+"?"+parameters.Encode(), &value); err != nil {
		return Metadata{}, err
	}
	name := musicBrainzName(value, selection.Type)
	if !musicBrainzID(value.ID) || name == "" {
		return Metadata{}, ErrUnavailable
	}
	selection.Provider = "musicbrainz"
	selection.ID = strings.ToLower(value.ID)
	fields := map[string]json.RawMessage{
		"Name": field(name),
		"ProviderIds": field(map[string]string{
			"MusicBrainz": selection.ID,
			providerKey:   selection.ID,
		}),
	}
	if sortName := strings.TrimSpace(value.SortName); sortName != "" {
		fields["SortName"] = field(sortName)
	}
	overview := strings.TrimSpace(value.Annotation)
	if overview == "" {
		overview = strings.TrimSpace(value.Disambiguation)
	}
	if overview != "" {
		fields["Overview"] = field(overview)
	}
	if year := musicBrainzYear(value.FirstReleaseDate); year > 0 {
		fields["ProductionYear"] = field(year)
	}
	// Partial MusicBrainz dates must not invent a month or day.
	if date, err := time.Parse("2006-01-02", value.FirstReleaseDate); err == nil && date.Year() > 0 {
		fields["PremiereDate"] = field(date.UTC())
	}
	if genres := musicBrainzTagNames(value.Genres); len(genres) > 0 {
		fields["Genres"] = field(genres)
	}
	if tags := musicBrainzTagNames(value.Tags); len(tags) > 0 {
		fields["Tags"] = field(tags)
	}
	if artists := musicBrainzArtists(value.ArtistCredit); len(artists) > 0 {
		fields["Artists"] = field(artists)
		if selection.Type == "MusicAlbum" {
			fields["AlbumArtists"] = field(artists)
		}
	}
	if people := musicBrainzPeople(value.Relations); len(people) > 0 {
		fields["People"] = field(people)
	}
	return Metadata{Selection: selection, Fields: fields, SourceURL: "https://musicbrainz.org/" + entity + "/" + selection.ID}, nil
}

func (client *Client) requestMusicBrainz(ctx context.Context, path string, target any) error {
	// The configured value must identify the application and its maintainers;
	// using a generic HTTP client User-Agent is not permitted by MusicBrainz.
	userAgent := strings.TrimSpace(client.config.MusicBrainzUserAgent)
	if userAgent == "" || len(userAgent) > 1024 || strings.ContainsAny(userAgent, "\r\n\x00") {
		return ErrNotConfigured
	}
	select {
	case musicBrainzRequests.gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-musicBrainzRequests.gate }()
	if delay := time.Until(musicBrainzRequests.next); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Wait after completion so DNS, shared connection admission, and scheduling
	// cannot compress the interval between requests actually reaching the API.
	defer func() { musicBrainzRequests.next = time.Now().Add(time.Second) }()
	headers := http.Header{"User-Agent": {userAgent}, "Accept": {"application/json"}}
	requestClient := *client
	httpClient := *client.http
	// A redirect is another request and must not bypass the shared rate gate.
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	requestClient.http = &httpClient
	return requestClient.requestJSON(ctx, http.MethodGet, musicBrainzBaseURL+path, headers, nil, target)
}

func musicBrainzEntityType(itemType string) (entity, providerKey string, ok bool) {
	switch itemType {
	case "MusicAlbum":
		return "release-group", "MusicBrainzReleaseGroup", true
	case "MusicArtist":
		return "artist", "MusicBrainzArtist", true
	case "Audio":
		return "recording", "MusicBrainzRecording", true
	default:
		return "", "", false
	}
}

func musicBrainzName(value musicBrainzEntity, itemType string) string {
	if itemType == "MusicArtist" {
		return strings.TrimSpace(value.Name)
	}
	return strings.TrimSpace(value.Title)
}

func musicBrainzID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}

func musicBrainzSearchPhrase(value string) string {
	var result strings.Builder
	result.WriteByte('"')
	for _, character := range strings.TrimSpace(value) {
		if strings.ContainsRune(`+-&|!(){}[]^"~*?:\/`, character) {
			result.WriteByte('\\')
		}
		result.WriteRune(character)
	}
	result.WriteByte('"')
	return result.String()
}

func musicBrainzScore(raw json.RawMessage) float64 {
	value := strings.TrimSpace(string(raw))
	if strings.HasPrefix(value, `"`) {
		if err := json.Unmarshal(raw, &value); err != nil {
			return 0
		}
	}
	score, err := strconv.ParseFloat(value, 64)
	if err != nil || !(score >= 0 && score <= 100) {
		return 0
	}
	return score
}

func musicBrainzYear(value string) int {
	if len(value) != 4 && len(value) != 7 && len(value) != 10 {
		return 0
	}
	layout := "2006"
	if len(value) == 7 {
		layout = "2006-01"
	} else if len(value) == 10 {
		layout = "2006-01-02"
	}
	date, err := time.Parse(layout, value)
	if err != nil || date.Year() < 1 || date.Year() > 9999 {
		return 0
	}
	return date.Year()
}

func musicBrainzTagNames(values []musicBrainzTag) []string {
	var names []string
	seen := make(map[string]bool)
	for _, value := range values {
		name := strings.TrimSpace(value.Name)
		if name != "" && value.Count >= 0 && !seen[name] {
			seen[name] = true
			names = append(names, name)
			if len(names) == 1024 {
				break
			}
		}
	}
	return names
}

func musicBrainzArtists(credits []musicBrainzArtistCredit) []string {
	var names []string
	seen := make(map[string]bool)
	for _, credit := range credits {
		name := strings.TrimSpace(credit.Name)
		if name == "" {
			name = strings.TrimSpace(credit.Artist.Name)
		}
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
			if len(names) == 1024 {
				break
			}
		}
	}
	return names
}

func musicBrainzPeople(relations []musicBrainzRelation) []metadata.Person {
	var people []metadata.Person
	seen := make(map[string]bool)
	appendPeople := func(values []musicBrainzRelation) {
		for _, relation := range values {
			if len(people) == 1024 {
				return
			}
			if relation.Artist == nil {
				continue
			}
			name := strings.TrimSpace(relation.Artist.Name)
			if name == "" {
				continue
			}
			var personType string
			switch relation.Type {
			case "composer":
				personType = "Composer"
			case "lyricist":
				personType = "Lyricist"
			case "conductor":
				personType = "Conductor"
			case "producer":
				personType = "Producer"
			case "writer":
				personType = "Writer"
			default:
				continue
			}
			role := strings.Join(relation.Attributes, ", ")
			key := name + "\x00" + personType + "\x00" + role
			if seen[key] {
				continue
			}
			seen[key] = true
			order := len(people)
			people = append(people, metadata.Person{Name: name, Type: personType, Role: role, SortOrder: &order})
		}
	}
	appendPeople(relations)
	for _, relation := range relations {
		if relation.Work != nil {
			appendPeople(relation.Work.Relations)
		}
	}
	return people
}

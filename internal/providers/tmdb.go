package providers

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	tmdbAPIBase       = "https://api.themoviedb.org/3"
	tmdbImageBase     = "https://image.tmdb.org/t/p/"
	tmdbMaximumImages = 200
)

type tmdbTarget struct {
	selection Selection
	path      string
	seriesID  int64
	season    int
	episode   int
}

type tmdbNamed struct {
	Name string `json:"name"`
}

type tmdbCredit struct {
	Name      string `json:"name"`
	Character string `json:"character"`
	Job       string `json:"job"`
	Order     *int   `json:"order"`
}

type tmdbCredits struct {
	Cast       []tmdbCredit `json:"cast"`
	Crew       []tmdbCredit `json:"crew"`
	GuestStars []tmdbCredit `json:"guest_stars"`
}

type tmdbRelease struct {
	Certification string `json:"certification"`
	Type          int    `json:"type"`
}

type tmdbDetail struct {
	ID                  int64        `json:"id"`
	Title               string       `json:"title"`
	Name                string       `json:"name"`
	OriginalTitle       string       `json:"original_title"`
	OriginalName        string       `json:"original_name"`
	Overview            string       `json:"overview"`
	ReleaseDate         string       `json:"release_date"`
	FirstAirDate        string       `json:"first_air_date"`
	AirDate             string       `json:"air_date"`
	IMDBID              string       `json:"imdb_id"`
	VoteAverage         *float64     `json:"vote_average"`
	SeasonNumber        *int         `json:"season_number"`
	EpisodeNumber       *int         `json:"episode_number"`
	Genres              []tmdbNamed  `json:"genres"`
	ProductionCompanies []tmdbNamed  `json:"production_companies"`
	Credits             tmdbCredits  `json:"credits"`
	GuestStars          []tmdbCredit `json:"guest_stars"`
	Crew                []tmdbCredit `json:"crew"`
	ExternalIDs         struct {
		IMDBID string `json:"imdb_id"`
		TVDBID int64  `json:"tvdb_id"`
	} `json:"external_ids"`
	ReleaseDates struct {
		Results []struct {
			Country  string        `json:"iso_3166_1"`
			Releases []tmdbRelease `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
	ContentRatings struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Rating  string `json:"rating"`
		} `json:"results"`
	} `json:"content_ratings"`
}

func (client *Client) tmdbHeaders() (http.Header, error) {
	token := strings.TrimSpace(client.config.TMDBToken)
	if token == "" {
		return nil, ErrNotConfigured
	}
	return http.Header{"Authorization": {"Bearer " + token}, "Accept": {"application/json"}}, nil
}

func (client *Client) tmdbLanguage(requested string) (string, error) {
	language := strings.TrimSpace(requested)
	if language == "" {
		language = strings.TrimSpace(client.config.Language)
	}
	if language == "" {
		return "en-US", nil
	}
	if len(language) > 35 {
		return "", ErrInvalidInput
	}
	parts := strings.Split(language, "-")
	for index, part := range parts {
		if part == "" || len(part) > 8 {
			return "", ErrInvalidInput
		}
		for _, character := range part {
			if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || index > 0 && character >= '0' && character <= '9' {
				continue
			}
			return "", ErrInvalidInput
		}
	}
	if len(parts[0]) < 2 || len(parts[0]) > 3 {
		return "", ErrInvalidInput
	}
	return language, nil
}

func (client *Client) tmdbCountry() string {
	country := strings.ToUpper(strings.TrimSpace(client.config.Country))
	if len(country) != 2 || country[0] < 'A' || country[0] > 'Z' || country[1] < 'A' || country[1] > 'Z' {
		return ""
	}
	return country
}

func tmdbNumber(value string, allowZero bool, maximum int64) (int64, error) {
	if len(value) == 0 || len(value) > 19 {
		return 0, ErrInvalidInput
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, ErrInvalidInput
		}
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil || number > maximum || number == 0 && !allowZero {
		return 0, ErrInvalidInput
	}
	return number, nil
}

func (client *Client) tmdbTarget(selection Selection) (tmdbTarget, error) {
	if selection.Provider != "tmdb" {
		return tmdbTarget{}, ErrInvalidInput
	}
	language, err := client.tmdbLanguage(selection.Language)
	if err != nil {
		return tmdbTarget{}, err
	}
	parts := strings.Split(selection.ID, ":")
	expected := 1
	resource := "tv"
	switch selection.Type {
	case "Movie", "Video":
		resource = "movie"
	case "Series":
	case "Season":
		expected = 2
	case "Episode":
		expected = 3
	default:
		return tmdbTarget{}, ErrInvalidInput
	}
	if len(parts) != expected {
		return tmdbTarget{}, ErrInvalidInput
	}
	id, err := tmdbNumber(parts[0], false, math.MaxInt64)
	if err != nil {
		return tmdbTarget{}, err
	}
	parts[0] = strconv.FormatInt(id, 10)
	target := tmdbTarget{selection: selection, path: "/" + resource + "/" + parts[0], seriesID: id}
	if expected >= 2 {
		season, err := tmdbNumber(parts[1], true, math.MaxInt32)
		if err != nil {
			return tmdbTarget{}, err
		}
		target.season = int(season)
		parts[1] = strconv.FormatInt(season, 10)
		target.path += "/season/" + parts[1]
	}
	if expected == 3 {
		episode, err := tmdbNumber(parts[2], false, math.MaxInt32)
		if err != nil {
			return tmdbTarget{}, err
		}
		target.episode = int(episode)
		parts[2] = strconv.FormatInt(episode, 10)
		target.path += "/episode/" + parts[2]
	}
	target.selection.ID = strings.Join(parts, ":")
	target.selection.Language = language
	return target, nil
}

func (client *Client) searchTMDB(ctx context.Context, query Query) ([]Match, error) {
	headers, err := client.tmdbHeaders()
	if err != nil {
		return nil, err
	}
	language, err := client.tmdbLanguage(query.Language)
	if err != nil {
		return nil, err
	}
	resource := "tv"
	switch query.Type {
	case "Movie", "Video":
		resource = "movie"
	case "Series":
	case "Season", "Episode":
		if query.Season < 0 || query.Season > math.MaxInt32 || query.Type == "Episode" && (query.Episode < 1 || query.Episode > math.MaxInt32) {
			return nil, ErrInvalidInput
		}
	default:
		return nil, ErrInvalidInput
	}
	parameters := url.Values{"query": {strings.TrimSpace(query.Name)}, "language": {language}, "include_adult": {"false"}, "page": {"1"}}
	if query.Year > 0 {
		if resource == "movie" {
			parameters.Set("year", strconv.Itoa(query.Year))
		} else if query.Type == "Series" {
			// A season or episode year is not necessarily the series premiere year.
			parameters.Set("first_air_date_year", strconv.Itoa(query.Year))
		}
	}
	if country := client.tmdbCountry(); country != "" && resource == "movie" {
		parameters.Set("region", country)
	}
	var response struct {
		Results []struct {
			ID            int64   `json:"id"`
			Title         string  `json:"title"`
			Name          string  `json:"name"`
			OriginalTitle string  `json:"original_title"`
			OriginalName  string  `json:"original_name"`
			Overview      string  `json:"overview"`
			ReleaseDate   string  `json:"release_date"`
			FirstAirDate  string  `json:"first_air_date"`
			Popularity    float64 `json:"popularity"`
		} `json:"results"`
	}
	if err := client.requestJSON(ctx, http.MethodGet, tmdbAPIBase+"/search/"+resource+"?"+parameters.Encode(), headers, nil, &response); err != nil {
		return nil, err
	}
	matches := make([]Match, 0, len(response.Results))
	for _, result := range response.Results {
		if result.ID <= 0 || len(matches) >= 20 {
			continue
		}
		name, original, date := result.Name, result.OriginalName, result.FirstAirDate
		if resource == "movie" {
			name, original, date = result.Title, result.OriginalTitle, result.ReleaseDate
		}
		name = tmdbText(name, 1024)
		if name == "" {
			continue
		}
		id := strconv.FormatInt(result.ID, 10)
		if query.Type == "Season" || query.Type == "Episode" {
			id += ":" + strconv.Itoa(query.Season)
		}
		if query.Type == "Episode" {
			id += ":" + strconv.Itoa(query.Episode)
		}
		match := Match{Selection: Selection{Provider: "tmdb", ID: id, Type: query.Type, Language: language}, Name: name,
			OriginalTitle: tmdbText(original, 64<<10), Overview: tmdbText(result.Overview, 64<<10), Score: result.Popularity}
		if premiere, ok := tmdbDate(date); ok {
			match.Year = premiere.Year()
		}
		matches = append(matches, match)
	}
	return matches, nil
}

func (client *Client) lookupTMDB(ctx context.Context, selection Selection) (Metadata, error) {
	headers, err := client.tmdbHeaders()
	if err != nil {
		return Metadata{}, err
	}
	target, err := client.tmdbTarget(selection)
	if err != nil {
		return Metadata{}, err
	}
	appendices := "external_ids,credits"
	if selection.Type == "Movie" || selection.Type == "Video" {
		appendices += ",release_dates"
	} else if selection.Type == "Series" {
		appendices += ",content_ratings"
	}
	parameters := url.Values{"language": {target.selection.Language}, "append_to_response": {appendices}}
	var detail tmdbDetail
	if err := client.requestJSON(ctx, http.MethodGet, tmdbAPIBase+target.path+"?"+parameters.Encode(), headers, nil, &detail); err != nil {
		return Metadata{}, err
	}
	if detail.ID <= 0 || (selection.Type == "Movie" || selection.Type == "Video" || selection.Type == "Series") && detail.ID != target.seriesID {
		return Metadata{}, ErrUnavailable
	}
	if selection.Type == "Season" || selection.Type == "Episode" {
		if detail.SeasonNumber != nil && *detail.SeasonNumber != target.season {
			return Metadata{}, ErrUnavailable
		}
	}
	if selection.Type == "Episode" && detail.EpisodeNumber != nil && *detail.EpisodeNumber != target.episode {
		return Metadata{}, ErrUnavailable
	}
	fields := make(map[string]json.RawMessage)
	name, original, date := detail.Name, detail.OriginalName, detail.FirstAirDate
	if selection.Type == "Movie" || selection.Type == "Video" {
		name, original, date = detail.Title, detail.OriginalTitle, detail.ReleaseDate
	} else if selection.Type == "Season" || selection.Type == "Episode" {
		date = detail.AirDate
	}
	if name = tmdbText(name, 1024); name != "" {
		fields["Name"], fields["SortName"] = field(name), field(name)
	}
	if text := tmdbText(original, 64<<10); text != "" {
		fields["OriginalTitle"] = field(text)
	}
	if text := tmdbText(detail.Overview, 64<<10); text != "" {
		fields["Overview"] = field(text)
	}
	if premiere, ok := tmdbDate(date); ok {
		fields["ProductionYear"], fields["PremiereDate"] = field(premiere.Year()), field(premiere)
	}
	if rating := detail.VoteAverage; rating != nil && *rating >= 0 && *rating <= 10 && !math.IsNaN(*rating) && !math.IsInf(*rating, 0) {
		fields["CommunityRating"] = field(*rating)
	}
	providerIDs := map[string]string{"Tmdb": target.selection.ID}
	imdb := detail.ExternalIDs.IMDBID
	if imdb == "" {
		imdb = detail.IMDBID
	}
	if strings.HasPrefix(imdb, "tt") {
		if _, err := tmdbNumber(strings.TrimPrefix(imdb, "tt"), false, math.MaxInt64); err == nil {
			providerIDs["Imdb"] = imdb
		}
	}
	if detail.ExternalIDs.TVDBID > 0 {
		providerIDs["Tvdb"] = strconv.FormatInt(detail.ExternalIDs.TVDBID, 10)
	}
	fields["ProviderIds"] = field(providerIDs)
	if names := tmdbNames(detail.Genres); len(names) > 0 {
		fields["Genres"] = field(names)
	}
	if names := tmdbNames(detail.ProductionCompanies); len(names) > 0 {
		fields["Studios"] = field(names)
	}
	if people := tmdbPeople(detail); len(people) > 0 {
		fields["People"] = field(people)
	}
	if country := client.tmdbCountry(); country != "" {
		if rating := tmdbOfficialRating(detail, country); rating != "" {
			fields["OfficialRating"] = field(rating)
		}
	}
	if selection.Type == "Season" {
		fields["IndexNumber"] = field(target.season)
	} else if selection.Type == "Episode" {
		fields["IndexNumber"], fields["ParentIndexNumber"] = field(target.episode), field(target.season)
	}
	return Metadata{Selection: target.selection, Fields: fields, SourceURL: "https://www.themoviedb.org" + target.path}, nil
}

func tmdbText(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) > maximum || !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
		return ""
	}
	return value
}

func tmdbDate(value string) (time.Time, bool) {
	date, err := time.Parse("2006-01-02", value)
	if err != nil {
		date, err = time.Parse(time.RFC3339Nano, value)
	}
	date = date.UTC()
	return date, err == nil && date.Year() >= 1 && date.Year() <= 9999
}

func tmdbNames(entries []tmdbNamed) []string {
	names := make([]string, 0)
	seen := make(map[string]bool)
	for _, entry := range entries {
		name := tmdbText(entry.Name, 1024)
		if name != "" && !seen[name] && len(names) < 128 {
			names = append(names, name)
			seen[name] = true
		}
	}
	return names
}

type tmdbPerson struct {
	Name      string `json:"Name"`
	Role      string `json:"Role,omitempty"`
	Type      string `json:"Type"`
	SortOrder *int   `json:"SortOrder,omitempty"`
}

func tmdbPeople(detail tmdbDetail) []tmdbPerson {
	people := make([]tmdbPerson, 0)
	seen := make(map[string]bool)
	add := func(credit tmdbCredit, kind, role string) {
		name := tmdbText(credit.Name, 1024)
		role = tmdbText(role, 1024)
		key := name + "\x00" + kind + "\x00" + role
		if name == "" || seen[key] || len(people) >= 256 {
			return
		}
		person := tmdbPerson{Name: name, Type: kind, Role: role}
		if credit.Order != nil && *credit.Order >= 0 && *credit.Order <= math.MaxInt32 {
			person.SortOrder = credit.Order
		}
		people = append(people, person)
		seen[key] = true
	}
	for _, credit := range detail.Credits.Cast {
		add(credit, "Actor", credit.Character)
	}
	for _, credits := range [][]tmdbCredit{detail.Credits.GuestStars, detail.GuestStars} {
		for _, credit := range credits {
			add(credit, "GuestStar", credit.Character)
		}
	}
	for _, credits := range [][]tmdbCredit{detail.Credits.Crew, detail.Crew} {
		for _, credit := range credits {
			kind := ""
			switch credit.Job {
			case "Director", "Co-Director", "Series Director":
				kind = "Director"
			case "Writer", "Screenplay", "Story", "Teleplay", "Novel", "Adaptation", "Original Story", "Story Writer", "Comic Book", "Characters":
				kind = "Writer"
			case "Producer", "Executive Producer", "Co-Producer", "Associate Producer", "Line Producer", "Co-Executive Producer", "Supervising Producer":
				kind = "Producer"
			case "Original Music Composer", "Composer", "Music":
				kind = "Composer"
			case "Conductor":
				kind = "Conductor"
			case "Lyricist":
				kind = "Lyricist"
			}
			if kind != "" {
				add(credit, kind, credit.Job)
			}
		}
	}
	return people
}

func tmdbOfficialRating(detail tmdbDetail, country string) string {
	for _, result := range detail.ContentRatings.Results {
		if result.Country == country {
			if rating := tmdbText(result.Rating, 1024); rating != "" {
				return rating
			}
		}
	}
	for _, result := range detail.ReleaseDates.Results {
		if result.Country != country {
			continue
		}
		// Prefer theatrical classifications before other release formats.
		for _, kind := range []int{3, 2, 4, 5, 6, 1} {
			for _, release := range result.Releases {
				if release.Type == kind {
					if rating := tmdbText(release.Certification, 1024); rating != "" {
						return rating
					}
				}
			}
		}
	}
	return ""
}

func tmdbImagePath(value string) bool {
	if len(value) < 6 || len(value) > 256 || value[0] != '/' {
		return false
	}
	dot := strings.LastIndexByte(value, '.')
	if dot < 2 {
		return false
	}
	switch value[dot:] {
	case ".jpg", ".jpeg", ".png", ".webp":
	default:
		return false
	}
	for _, character := range value[1:dot] {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' {
			continue
		}
		return false
	}
	return true
}

func (client *Client) imagesTMDB(ctx context.Context, selection Selection) ([]RemoteImage, error) {
	headers, err := client.tmdbHeaders()
	if err != nil {
		return nil, err
	}
	target, err := client.tmdbTarget(selection)
	if err != nil {
		return nil, err
	}
	language := strings.ToLower(strings.SplitN(target.selection.Language, "-", 2)[0])
	parameters := url.Values{"include_image_language": {language + ",null"}}
	type imageEntry struct {
		Path   string `json:"file_path"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	var response struct {
		Posters   []imageEntry `json:"posters"`
		Backdrops []imageEntry `json:"backdrops"`
		Stills    []imageEntry `json:"stills"`
	}
	if err := client.requestJSON(ctx, http.MethodGet, tmdbAPIBase+target.path+"/images?"+parameters.Encode(), headers, nil, &response); err != nil {
		return nil, err
	}
	images := make([]RemoteImage, 0)
	seen := make(map[string]bool)
	for _, group := range []struct {
		kind    string
		preview string
		entries []imageEntry
	}{{"Primary", "w500", response.Posters}, {"Backdrop", "w780", response.Backdrops}, {"Thumb", "w300", response.Stills}} {
		for _, entry := range group.entries {
			key := group.kind + ":" + entry.Path
			if !tmdbImagePath(entry.Path) || entry.Width <= 0 || entry.Height <= 0 || entry.Width > 32768 || entry.Height > 32768 || seen[key] || len(images) >= tmdbMaximumImages {
				continue
			}
			images = append(images, RemoteImage{Selection: target.selection, ImageID: entry.Path, ImageType: group.kind,
				Width: entry.Width, Height: entry.Height, PreviewURL: tmdbImageBase + group.preview + entry.Path})
			seen[key] = true
		}
	}
	return images, nil
}

// DownloadImage resolves the selected image again; supplied preview URLs and
// dimensions are never trusted as download authority.
func (client *Client) DownloadImage(ctx context.Context, selected RemoteImage) ([]byte, error) {
	if !client.config.Enabled {
		return nil, ErrDisabled
	}
	if selected.Provider != "tmdb" || !tmdbImagePath(selected.ImageID) {
		return nil, ErrInvalidInput
	}
	switch selected.ImageType {
	case "Primary", "Backdrop", "Thumb":
	default:
		return nil, ErrInvalidInput
	}
	images, err := client.imagesTMDB(ctx, selected.Selection)
	if err != nil {
		return nil, err
	}
	for _, candidate := range images {
		if candidate.ImageID != selected.ImageID || candidate.ImageType != selected.ImageType {
			continue
		}
		data, _, err := client.requestBytes(ctx, http.MethodGet, tmdbImageBase+"original"+candidate.ImageID,
			http.Header{"Accept": {"image/jpeg, image/png, image/webp"}}, nil, 20<<20)
		if err != nil {
			return nil, err
		}
		switch http.DetectContentType(data) {
		case "image/jpeg", "image/png", "image/webp":
			return data, nil
		default:
			return nil, ErrUnavailable
		}
	}
	return nil, ErrNotFound
}

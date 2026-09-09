package server

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/metadata"
)

func (s *Server) itemUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	for _, name := range []string{"EnableImages", "EnableUserData"} {
		if raw := r.URL.Query().Get(name); raw != "" {
			if _, err := strconv.ParseBool(raw); err != nil {
				apiError(w, r, 400, "invalid_input", name+" must be a boolean.")
				return "", false
			}
		}
	}
	principal := r.Context().Value(principalKey).(identity.Principal)
	userID := r.PathValue("UserId")
	queryUserID := r.URL.Query().Get("UserId")
	if userID != "" && queryUserID != "" && userID != queryUserID {
		apiError(w, r, 400, "invalid_input", "User identifiers must agree.")
		return "", false
	}
	if userID == "" {
		userID = queryUserID
	}
	if userID == "" {
		userID = principal.User.ID
	}
	if userID != principal.User.ID && !principal.User.IsAdministrator {
		apiError(w, r, 403, "access_denied", "The requested user's library is not accessible.")
		return "", false
	}
	return userID, true
}

func virtualRootID() string { return "goby-library-root" }

func (s *Server) embyViews(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	libraries, err := s.library.ListUserLibraries(r.Context(), userID)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(libraries))
	for _, entry := range libraries {
		item := s.itemDTO(library.Item{ID: entry.ID, LibraryID: entry.ID, Name: entry.Name, SortName: entry.Name, Type: "CollectionFolder", IsFolder: true, CreatedAt: entry.CreatedAt}, nil, false)
		if entry.CollectionType == "mixed" {
			item["CollectionType"] = nil
		} else {
			item["CollectionType"] = entry.CollectionType
		}
		items = append(items, item)
	}
	if !s.applyIndexedImages(w, r, userID, items, false) {
		return
	}
	for _, item := range items {
		applyItemSwitches(item, r)
	}
	jsonResponse(w, 200, map[string]any{"Items": items, "TotalRecordCount": len(items)})
}

func (s *Server) embyRoot(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	libraries, err := s.library.ListUserLibraries(r.Context(), userID)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"Id": virtualRootID(), "Name": "Media libraries", "Type": "Folder", "IsFolder": true, "ServerId": s.serverID, "ChildCount": len(libraries)})
}

func readItemQuery(w http.ResponseWriter, r *http.Request, userID string) (library.Query, bool) {
	values := r.URL.Query()
	query := library.Query{UserID: userID, ParentID: values.Get("ParentId"), SearchTerm: values.Get("SearchTerm"), SortBy: values.Get("SortBy"), SortOrder: values.Get("SortOrder"), Limit: 100}
	if query.ParentID == virtualRootID() {
		query.ParentID = ""
	}
	if raw := values.Get("Recursive"); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			apiError(w, r, 400, "invalid_input", "Recursive must be a boolean.")
			return query, false
		}
		query.Recursive = value
	}
	for name, target := range map[string]*int{"StartIndex": &query.StartIndex, "Limit": &query.Limit} {
		if raw := values.Get(name); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 0 {
				apiError(w, r, 400, "invalid_input", "Pagination must use non-negative integers.")
				return query, false
			}
			*target = value
		}
	}
	if query.Limit > 1000 {
		query.Limit = 1000
	}
	query.IncludeItemTypes = queryValues(values["IncludeItemTypes"])
	query.Ids = queryValues(values["Ids"])
	query.MediaTypes = queryValues(values["MediaTypes"])
	for _, flag := range []struct {
		name   string
		target **bool
	}{{"IsPlayed", &query.IsPlayed}, {"IsFavorite", &query.IsFavorite}} {
		if raw := values.Get(flag.name); raw != "" {
			value, err := strconv.ParseBool(raw)
			if err != nil {
				apiError(w, r, 400, "invalid_input", flag.name+" must be a boolean.")
				return query, false
			}
			*flag.target = &value
		}
	}
	for _, filter := range queryValues(values["Filters"]) {
		var target **bool
		var value bool
		switch strings.ToLower(filter) {
		case "isplayed":
			target, value = &query.IsPlayed, true
		case "isunplayed":
			target, value = &query.IsPlayed, false
		case "isfavorite", "isfavoriteorlikes":
			target, value = &query.IsFavorite, true
		case "isresumable":
			query.Resumable = true
			continue
		default:
			apiError(w, r, 400, "unsupported_filter", "The requested user-data filter is not supported.")
			return query, false
		}
		if *target != nil && **target != value {
			apiError(w, r, 400, "invalid_input", "User-data filters must agree.")
			return query, false
		}
		*target = &value
	}
	if !readEntityFilters(w, r, &query) {
		return query, false
	}
	return query, true
}

func queryValues(values []string) []string {
	var result []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if part = strings.TrimSpace(part); part != "" {
				result = append(result, part)
			}
		}
	}
	return result
}

func (s *Server) embyItems(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	query, ok := readItemQuery(w, r, userID)
	if !ok {
		return
	}
	s.sendItemQuery(w, r, query, false)
}

func (s *Server) sendItemQuery(w http.ResponseWriter, r *http.Request, query library.Query, bare bool) {
	zeroLimit := query.Limit == 0
	if zeroLimit {
		query.Limit = 1
	}
	result, err := s.library.QueryItems(r.Context(), query)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	fields := queryValues(r.URL.Query()["Fields"])
	items := make([]map[string]any, 0, len(result.Items))
	if !zeroLimit {
		for _, entry := range result.Items {
			item := s.itemDTO(entry, fields, false)
			if entry.Type == "CollectionFolder" {
				lib, err := s.library.GetLibrary(r.Context(), entry.LibraryID)
				if err != nil {
					s.libraryError(w, r, err)
					return
				}
				if lib.CollectionType == "mixed" {
					item["CollectionType"] = nil
				} else {
					item["CollectionType"] = lib.CollectionType
				}
			}
			applyItemSwitches(item, r)
			items = append(items, item)
		}
	}
	if !s.applyIndexedImages(w, r, query.UserID, items, false) {
		return
	}
	if bare {
		jsonResponse(w, 200, items)
		return
	}
	jsonResponse(w, 200, map[string]any{"Items": items, "TotalRecordCount": result.TotalRecordCount})
}

func (s *Server) embyItem(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	if r.PathValue("Id") == virtualRootID() {
		s.embyRoot(w, r)
		return
	}
	item, err := s.library.GetItem(r.Context(), userID, r.PathValue("Id"))
	if err != nil {
		if errors.Is(err, library.ErrNotFound) {
			if id, valid := positiveEntityID(r.PathValue("Id")); valid {
				entity, entityErr := s.library.GetEntityByID(r.Context(), userID, id)
				if entityErr != nil {
					s.libraryError(w, r, entityErr)
					return
				}
				dto := s.entityDTO(entity, queryValues(r.URL.Query()["Fields"]), true)
				if entity.Type == "Person" {
					dto["TagItems"] = []map[string]any{}
				}
				applyItemSwitches(dto, r)
				jsonResponse(w, 200, dto)
				return
			}
		}
		s.libraryError(w, r, err)
		return
	}
	dto := s.itemDTO(item, queryValues(r.URL.Query()["Fields"]), true)
	if item.Type == "CollectionFolder" {
		lib, err := s.library.GetLibrary(r.Context(), item.LibraryID)
		if err != nil {
			s.libraryError(w, r, err)
			return
		}
		if lib.CollectionType == "mixed" {
			dto["CollectionType"] = nil
		} else {
			dto["CollectionType"] = lib.CollectionType
		}
	}
	if !s.applyIndexedImages(w, r, userID, []map[string]any{dto}, true) {
		return
	}
	applyItemSwitches(dto, r)
	jsonResponse(w, 200, dto)
}

func (s *Server) embyLatest(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	query, ok := readItemQuery(w, r, userID)
	if !ok {
		return
	}
	query.Recursive = true
	query.SortBy = "DateCreated"
	query.SortOrder = "Descending"
	if r.URL.Query().Get("Limit") == "" {
		query.Limit = 20
	}
	if len(query.IncludeItemTypes) == 0 {
		query.IncludeItemTypes = []string{"Movie", "Episode", "Audio", "Video"}
	}
	group := true
	if raw := r.URL.Query().Get("GroupItems"); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			apiError(w, r, 400, "invalid_input", "GroupItems must be a boolean.")
			return
		}
		group = value
	}
	zeroLimit := query.Limit == 0
	if zeroLimit {
		query.Limit = 1
	}
	result, err := s.library.QueryLatest(r.Context(), query, group)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(result))
	if !zeroLimit {
		for _, entry := range result {
			dto := s.itemDTO(entry.Item, queryValues(r.URL.Query()["Fields"]), false)
			if group && entry.Item.IsFolder {
				dto["ChildCount"] = entry.ChildCount
			}
			applyItemSwitches(dto, r)
			items = append(items, dto)
		}
	}
	if !s.applyIndexedImages(w, r, userID, items, false) {
		return
	}
	jsonResponse(w, 200, items)
}

func (s *Server) embySeasons(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	series, err := s.library.GetItem(r.Context(), userID, r.PathValue("Id"))
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	if series.Type != "Series" {
		apiError(w, r, 400, "invalid_input", "The item is not a television series.")
		return
	}
	query, ok := readItemQuery(w, r, userID)
	if !ok {
		return
	}
	query.ParentID = series.ID
	query.Recursive = false
	query.IncludeItemTypes = []string{"Season"}
	if query.SortBy == "" {
		query.SortBy = "IndexNumber"
	}
	s.sendItemQuery(w, r, query, false)
}

func (s *Server) embyEpisodes(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	series, err := s.library.GetItem(r.Context(), userID, r.PathValue("Id"))
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	if series.Type != "Series" {
		apiError(w, r, 400, "invalid_input", "The item is not a television series.")
		return
	}
	query, ok := readItemQuery(w, r, userID)
	if !ok {
		return
	}
	query.ParentID = series.ID
	query.Recursive = true
	query.IncludeItemTypes = []string{"Episode"}
	if query.SortBy == "" {
		query.SortBy = "IndexNumber"
	}
	if raw := r.URL.Query().Get("Season"); raw != "" {
		number, err := strconv.Atoi(raw)
		if err != nil || number < 0 || number > 2147483647 {
			apiError(w, r, 400, "invalid_input", "Season must be a non-negative 32-bit integer.")
			return
		}
		query.ParentIndexNumber = &number
	}
	if seasonID := r.URL.Query().Get("SeasonId"); seasonID != "" {
		season, err := s.library.GetItem(r.Context(), userID, seasonID)
		if err != nil {
			s.libraryError(w, r, err)
			return
		}
		if season.Type != "Season" || season.ParentID != series.ID {
			apiError(w, r, 400, "invalid_input", "The season does not belong to the requested series.")
			return
		}
		if query.ParentIndexNumber != nil && *query.ParentIndexNumber != season.IndexNumber {
			apiError(w, r, 400, "invalid_input", "Season identifiers must agree.")
			return
		}
		query.ParentID = season.ID
		query.Recursive = false
	}
	s.sendItemQuery(w, r, query, false)
}

func (s *Server) itemDTO(item library.Item, fields []string, detail bool) map[string]any {
	dto := map[string]any{"Id": item.ID, "Name": item.Name, "SortName": item.SortName, "Type": item.Type, "IsFolder": item.IsFolder, "ServerId": s.serverID, "DateCreated": item.CreatedAt, "ImageTags": map[string]string{}, "BackdropImageTags": []string{}}
	if (detail || hasField(fields, "Path")) && item.Path != "" {
		dto["Path"] = item.Path
	}
	if item.ParentID != "" {
		dto["ParentId"] = item.ParentID
	} else {
		dto["ParentId"] = virtualRootID()
	}
	if item.Type == "Season" || item.Type == "Episode" {
		dto["IndexNumber"] = item.IndexNumber
	}
	if item.Type == "Episode" {
		dto["ParentIndexNumber"] = item.ParentIndexNumber
	}
	if detail || hasField(fields, "Overview") {
		dto["Overview"] = item.Overview
	}
	addLocalMetadata(dto, item.Metadata, item.Entities, fields, detail)
	if item.UserData != nil {
		dto["UserData"] = userDataDTO(*item.UserData, false)
	}
	if item.Media != nil {
		mediaType := "Video"
		if item.Type == "Audio" {
			mediaType = "Audio"
		}
		dto["MediaType"] = mediaType
		dto["RunTimeTicks"] = item.Media.DurationTicks
		if mediaType == "Video" {
			dto["VideoType"] = "VideoFile"
		}
		if detail || hasField(fields, "MediaStreams") {
			dto["MediaStreams"] = mediaStreamsDTO(item.Media.Streams)
		}
		if detail || hasField(fields, "MediaSources") {
			dto["MediaSources"] = []map[string]any{originalSourceDTO(item)}
		}
		if detail || hasField(fields, "Chapters") {
			chapters := make([]map[string]any, 0, len(item.Media.Chapters))
			for _, chapter := range item.Media.Chapters {
				chapters = append(chapters, map[string]any{"StartPositionTicks": chapter.StartTicks, "Name": chapter.Title})
			}
			dto["Chapters"] = chapters
		}
	}
	return dto
}

// Descriptive metadata follows Fields/detail projection, as confirmed by the
// NFO reference captures. The default item list omits these metadata values.
// Display names, descriptions, and hierarchy indexes belong to the catalog item.
func addLocalMetadata(dto map[string]any, source *metadata.Metadata, entities library.ItemEntities, fields []string, detail bool) {
	var local metadata.Metadata
	if source != nil {
		local = *source
	}
	if local.ProductionYear != nil && (detail || hasField(fields, "ProductionYear")) {
		dto["ProductionYear"] = int32(*local.ProductionYear)
	}
	if local.PremiereDate != nil && (detail || hasField(fields, "PremiereDate")) {
		dto["PremiereDate"] = local.PremiereDate.UTC()
	}
	if local.OriginalTitle != "" && (detail || hasField(fields, "OriginalTitle")) {
		dto["OriginalTitle"] = local.OriginalTitle
	}
	if local.CommunityRating != nil && (detail || hasField(fields, "CommunityRating")) {
		dto["CommunityRating"] = float32(*local.CommunityRating)
	}
	if local.OfficialRating != "" && (detail || hasField(fields, "OfficialRating")) {
		dto["OfficialRating"] = local.OfficialRating
	}
	if detail || hasField(fields, "ProviderIds") {
		providers := make(map[string]string, len(local.ProviderIDs))
		for key, value := range local.ProviderIDs {
			providers[key] = value
		}
		dto["ProviderIds"] = providers
	}
	if detail || hasField(fields, "Genres") {
		genres := localMetadataRefs(local.Genres, entities.Genres)
		names := make([]string, 0, len(genres))
		for _, genre := range genres {
			names = append(names, genre.Name)
		}
		dto["Genres"] = names
		dto["GenreItems"] = metadataEntityDTOs(genres)
	}
	if detail || hasField(fields, "Tags") {
		tags := localMetadataRefs(local.Tags, entities.Tags)
		sort.SliceStable(tags, func(i, j int) bool {
			left, right := strings.ToLower(tags[i].Name), strings.ToLower(tags[j].Name)
			if left == right {
				return tags[i].Name < tags[j].Name
			}
			return left < right
		})
		dto["TagItems"] = metadataEntityDTOs(tags)
	}
	if detail || hasField(fields, "Studios") {
		dto["Studios"] = metadataEntityDTOs(localMetadataRefs(local.Studios, entities.Studios))
	}
	if detail || hasField(fields, "People") {
		credits := append([]library.PersonRef(nil), entities.People...)
		if len(credits) == 0 {
			for _, person := range local.People {
				credits = append(credits, library.PersonRef{Name: person.Name, Role: person.Role, Type: person.Type, SortOrder: person.SortOrder})
			}
		}
		sort.SliceStable(credits, func(i, j int) bool {
			left, right := credits[i].SortOrder, credits[j].SortOrder
			if left == nil {
				return false
			}
			return right == nil || *left < *right
		})
		people := make([]map[string]any, 0, len(credits))
		for _, credit := range credits {
			person := map[string]any{"Name": credit.Name, "Type": credit.Type}
			if credit.ID != "" {
				person["Id"] = credit.ID
			}
			if credit.Role != "" {
				person["Role"] = credit.Role
			}
			// BaseItemPerson has no SortOrder field. Preserve that order in the
			// array and expose only IDs supplied by persistent associations.
			people = append(people, person)
		}
		dto["People"] = people
	}
}

func localMetadataRefs(names []string, entities []library.EntityRef) []library.EntityRef {
	if len(entities) != 0 {
		return append([]library.EntityRef(nil), entities...)
	}
	items := make([]library.EntityRef, 0, len(names))
	for _, name := range names {
		items = append(items, library.EntityRef{Name: name})
	}
	return items
}

func metadataEntityDTOs(entities []library.EntityRef) []map[string]any {
	items := make([]map[string]any, 0, len(entities))
	for _, entity := range entities {
		item := map[string]any{"Name": entity.Name}
		if entity.ID > 0 {
			item["Id"] = entity.ID
		}
		items = append(items, item)
	}
	return items
}

func applyItemSwitches(item map[string]any, r *http.Request) {
	if value := r.URL.Query().Get("EnableImages"); value != "" {
		enabled, _ := strconv.ParseBool(value)
		if !enabled {
			delete(item, "ImageTags")
			delete(item, "BackdropImageTags")
		}
	}
	if value := r.URL.Query().Get("EnableUserData"); value != "" {
		enabled, _ := strconv.ParseBool(value)
		if !enabled {
			delete(item, "UserData")
		}
	}
}

func hasField(fields []string, name string) bool {
	for _, field := range fields {
		if strings.EqualFold(field, name) {
			return true
		}
	}
	return false
}

func mediaStreamsDTO(streams []media.Stream) []map[string]any {
	items := make([]map[string]any, 0, len(streams))
	for _, stream := range streams {
		kind := stream.CodecType
		switch kind {
		case "video":
			kind = "Video"
			if stream.IsAttachedPicture {
				kind = "EmbeddedImage"
			}
		case "audio":
			kind = "Audio"
		case "subtitle":
			kind = "Subtitle"
		case "attachment":
			kind = "Attachment"
		case "data":
			kind = "Data"
		}
		item := map[string]any{"Index": stream.Index, "Type": kind, "Codec": stream.Codec, "Language": stream.Language, "Title": stream.Title, "DisplayTitle": streamDisplayTitle(stream), "IsDefault": stream.IsDefault, "IsForced": stream.IsForced, "IsExternal": stream.IsExternal, "IsTextSubtitleStream": stream.IsTextSubtitleStream, "Profile": stream.Profile}
		if stream.Width > 0 {
			item["Width"] = stream.Width
		}
		if stream.Height > 0 {
			item["Height"] = stream.Height
		}
		if stream.Channels > 0 {
			item["Channels"] = stream.Channels
		}
		if stream.SampleRate > 0 {
			item["SampleRate"] = stream.SampleRate
		}
		if stream.Bitrate > 0 {
			item["BitRate"] = stream.Bitrate
		}
		if stream.Level > 0 {
			item["Level"] = stream.Level
		}
		if stream.PixelFormat != "" {
			item["PixelFormat"] = stream.PixelFormat
		}
		for key, value := range map[string]string{"TimeBase": stream.TimeBase, "ChannelLayout": stream.ChannelLayout,
			"ColorSpace": stream.ColorSpace, "ColorTransfer": stream.ColorTransfer, "ColorPrimaries": stream.ColorPrimaries} {
			if value != "" {
				item[key] = value
			}
		}
		if stream.CodecTagString != "" {
			item["CodecTag"] = stream.CodecTagString
		}
		if stream.BitDepth > 0 {
			item["BitDepth"] = stream.BitDepth
		}
		if stream.RefFrames > 0 {
			item["RefFrames"] = stream.RefFrames
		}
		if stream.InterlaceKnown {
			item["IsInterlaced"] = stream.IsInterlaced
		}
		if stream.VideoRangeKnown {
			item["VideoRange"] = stream.VideoRange
		}
		if numerator, denominator, ok := strings.Cut(stream.AverageFrameRate, "/"); ok {
			n, nerr := strconv.ParseFloat(numerator, 64)
			d, derr := strconv.ParseFloat(denominator, 64)
			if nerr == nil && derr == nil && d > 0 {
				item["AverageFrameRate"] = n / d
			}
		}
		if numerator, denominator, ok := strings.Cut(stream.RealFrameRate, "/"); ok {
			n, nerr := strconv.ParseFloat(numerator, 64)
			d, derr := strconv.ParseFloat(denominator, 64)
			if nerr == nil && derr == nil && d > 0 {
				item["RealFrameRate"] = n / d
			}
		}
		items = append(items, item)
	}
	return items
}

func streamDisplayTitle(stream media.Stream) string {
	if strings.TrimSpace(stream.Title) != "" {
		return stream.Title
	}
	parts := make([]string, 0, 4)
	if stream.Language != "" && stream.Language != "und" {
		parts = append(parts, strings.ToUpper(stream.Language))
	}
	if stream.Codec != "" {
		parts = append(parts, strings.ToUpper(stream.Codec))
	}
	if stream.Channels > 0 {
		parts = append(parts, strconv.Itoa(stream.Channels)+" ch")
	}
	if stream.IsForced {
		parts = append(parts, "Forced")
	} else if stream.IsDefault {
		parts = append(parts, "Default")
	}
	return strings.Join(parts, " / ")
}

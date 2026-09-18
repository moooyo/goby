package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/providers"
)

var subtitleSelectionKey struct {
	once sync.Once
	key  [32]byte
	err  error
}

type subtitleSelectionClaims struct {
	ItemID    string                   `json:"item"`
	SessionID string                   `json:"session"`
	SourceTag string                   `json:"source"`
	Expires   int64                    `json:"expires"`
	Selection providers.RemoteSubtitle `json:"selection"`
}

func subtitleSelectionMAC(data []byte) ([]byte, error) {
	subtitleSelectionKey.once.Do(func() { _, subtitleSelectionKey.err = rand.Read(subtitleSelectionKey.key[:]) })
	if subtitleSelectionKey.err != nil {
		return nil, library.ErrUnavailable
	}
	mac := hmac.New(sha256.New, subtitleSelectionKey.key[:])
	_, _ = mac.Write(data)
	return mac.Sum(nil), nil
}

func encodeSubtitleSelection(claims subtitleSelectionClaims) (string, error) {
	// Human labels are not needed to redeem an item- and session-bound result.
	claims.Selection.Name = ""
	data, err := json.Marshal(claims)
	if err != nil || len(data) > 2048 {
		return "", library.ErrInvalidInput
	}
	mac, err := subtitleSelectionMAC(data)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(mac), nil
}

func decodeSubtitleSelection(raw, itemID, sessionID string) (subtitleSelectionClaims, error) {
	var claims subtitleSelectionClaims
	if len(raw) > 3072 {
		return claims, library.ErrInvalidInput
	}
	encoded, signature, ok := strings.Cut(raw, ".")
	if !ok {
		return claims, library.ErrInvalidInput
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(data) > 2048 {
		return claims, library.ErrInvalidInput
	}
	provided, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || len(provided) != sha256.Size {
		return claims, library.ErrInvalidInput
	}
	expected, err := subtitleSelectionMAC(data)
	if err != nil {
		return claims, err
	}
	if !hmac.Equal(expected, provided) || json.Unmarshal(data, &claims) != nil || claims.ItemID != itemID || claims.SessionID != sessionID || claims.SourceTag == "" || claims.Expires <= time.Now().Unix() {
		return subtitleSelectionClaims{}, library.ErrInvalidInput
	}
	return claims, nil
}

func (s *Server) providerSubtitlePrincipal(w http.ResponseWriter, r *http.Request) (identity.Principal, bool) {
	actor := r.Context().Value(principalKey).(identity.Principal)
	if actor.IsApplicationKey() || actor.Kind != "emby" || actor.User.ID == "" {
		apiError(w, r, http.StatusForbidden, "user_required", "Use an authenticated user session to download subtitles.")
		return actor, false
	}
	return actor, true
}

func providerSubtitleParameters(r *http.Request, search bool) (string, map[string]bool, error) {
	values, err := streamValues(r)
	if err != nil {
		return "", nil, library.ErrInvalidInput
	}
	flags := map[string]bool{}
	for key, value := range values {
		switch key {
		case "mediasourceid", "api_key":
		case "isperfectmatch", "isforced", "ishearingimpaired":
			if !search {
				return "", nil, library.ErrInvalidInput
			}
			flag, err := strconv.ParseBool(value)
			if err != nil {
				return "", nil, library.ErrInvalidInput
			}
			flags[key] = flag
		default:
			return "", nil, library.ErrInvalidInput
		}
	}
	if values["mediasourceid"] == "" || len(values["mediasourceid"]) > 256 {
		return "", nil, library.ErrInvalidInput
	}
	return values["mediasourceid"], flags, nil
}

func (s *Server) embyProviderSubtitleSearch(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.providerSubtitlePrincipal(w, r)
	if !ok {
		return
	}
	sourceID, flags, err := providerSubtitleParameters(r, true)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	query, tag, err := s.library.SubtitleProviderTarget(ctx, actor, r.PathValue("Id"), sourceID)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	items, err := providers.New(s.onlineProviderConfig()).SearchSubtitles(ctx, providers.SubtitleQuery{Query: query, Languages: []string{r.PathValue("Language")}, HearingImpaired: flags["ishearingimpaired"]})
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	_, currentTag, err := s.library.SubtitleProviderTarget(ctx, actor, r.PathValue("Id"), sourceID)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	if currentTag != tag {
		s.providerError(w, r, library.ErrSourceChanged)
		return
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if flags["isperfectmatch"] && !item.MovieHashMatch || flags["isforced"] && !item.IsForced {
			continue
		}
		token, err := encodeSubtitleSelection(subtitleSelectionClaims{ItemID: r.PathValue("Id"), SessionID: actor.SessionID, SourceTag: tag, Expires: time.Now().Add(15 * time.Minute).Unix(), Selection: item})
		if err != nil {
			s.providerError(w, r, err)
			return
		}
		result = append(result, map[string]any{"Id": token, "ProviderName": "OpenSubtitles", "Name": item.Name, "Format": "srt", "Language": item.Language, "DownloadCount": item.DownloadCount, "IsHashMatch": item.MovieHashMatch, "IsForced": item.IsForced, "IsHearingImpaired": item.HearingImpaired})
	}
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) embyProviderSubtitleDownload(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.providerSubtitlePrincipal(w, r)
	if !ok {
		return
	}
	sourceID, _, err := providerSubtitleParameters(r, false)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	claims, err := decodeSubtitleSelection(r.PathValue("SubtitleId"), r.PathValue("Id"), actor.SessionID)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	_, tag, err := s.library.SubtitleProviderTarget(ctx, actor, claims.ItemID, sourceID)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	if tag != claims.SourceTag {
		s.providerError(w, r, library.ErrSourceChanged)
		return
	}
	download, err := providers.New(s.onlineProviderConfig()).DownloadSubtitle(ctx, claims.Selection)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	if err := s.library.RegisterDownloadedSubtitleForSource(ctx, actor, claims.ItemID, claims.SourceTag, download); err != nil {
		s.providerError(w, r, err)
		return
	}
	index, err := s.library.DownloadedSubtitleIndex(ctx, actor, claims.ItemID, download.RemoteID)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"NewIndex": index})
}

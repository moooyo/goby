package dynamicsource

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"

	"github.com/moooyo/goby/internal/media"
)

// ExternalSubtitleIndexBase separates configured sidecars from probed stream
// indices. These indices must never be passed to FFmpeg's media input mapper.
const ExternalSubtitleIndexBase = 1 << 20

// SubtitleDefinition is a trusted operator declaration, not a client URL.
// Document is a bounded finite resource; webvtt-stream is an incremental VTT
// response; webvtt-hls is a rolling VTT media playlist. Media clocks are source
// media timestamps, while mpegts clocks require an explicit X-TIMESTAMP-MAP.
// OffsetTicks is a declared alignment offset before a viewer's own offset.
type SubtitleDefinition struct {
	ID               string            `json:"id"`
	Name             string            `json:"name,omitempty"`
	Language         string            `json:"language,omitempty"`
	Format           string            `json:"format"`
	Mode             string            `json:"mode"`
	URL              string            `json:"url"`
	Headers          map[string]string `json:"headers,omitempty"`
	Clock            string            `json:"clock"`
	SegmentClock     string            `json:"segmentClock,omitempty"`
	StreamWatermarks string            `json:"streamWatermarks,omitempty"`
	OffsetTicks      int64             `json:"offsetTicks,omitempty"`
	Default          bool              `json:"default,omitempty"`
	Forced           bool              `json:"forced,omitempty"`
}

func (SubtitleDefinition) String() string   { return "<dynamic-subtitle configuration>" }
func (SubtitleDefinition) GoString() string { return "<dynamic-subtitle configuration>" }

// ValidateSubtitleDefinitions applies the same bounded private request rules
// as a registered media input, without fetching any remote resource.
func ValidateSubtitleDefinitions(definitions []SubtitleDefinition) error {
	if len(definitions) > 8 {
		return ErrInvalid
	}
	seen := make(map[string]bool, len(definitions))
	defaults := 0
	for _, definition := range definitions {
		if !validID(definition.ID, false) || !validID(definition.Name, true) ||
			!validID(definition.Language, true) || len(definition.Language) > 64 || seen[definition.ID] ||
			definition.OffsetTicks < -24*60*60*media.TicksPerSecond || definition.OffsetTicks > 24*60*60*media.TicksPerSecond {
			return ErrInvalid
		}
		seen[definition.ID] = true
		if definition.Default {
			defaults++
		}
		if defaults > 1 {
			return ErrInvalid
		}
		switch definition.Format {
		case "webvtt", "subrip", "ass":
		default:
			return ErrInvalid
		}
		switch definition.Mode {
		case "document":
			if definition.SegmentClock != "" || definition.StreamWatermarks != "" {
				return ErrInvalid
			}
		case "webvtt-stream":
			if definition.Format != "webvtt" || definition.StreamWatermarks != "goby-note-v1" || definition.SegmentClock != "" {
				return ErrInvalid
			}
		case "webvtt-hls":
			if definition.Format != "webvtt" || definition.Clock != "mpegts" || definition.SegmentClock != "timestamp-map" || definition.StreamWatermarks != "" {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
		if definition.Clock != "media" && definition.Clock != "mpegts" || definition.Clock == "mpegts" && definition.Format != "webvtt" {
			return ErrInvalid
		}
		if err := validateHTTPRequest(definition.URL, definition.Headers); err != nil {
			return err
		}
	}
	return nil
}

func cloneSubtitles(definitions []SubtitleDefinition) []SubtitleDefinition {
	result := append([]SubtitleDefinition(nil), definitions...)
	for index := range result {
		result[index].Headers = cloneHeaders(result[index].Headers)
	}
	return result
}

func subtitleTag(leaseID string, definition SubtitleDefinition) string {
	// encoding/json deterministically orders map keys. The declaration digest
	// binds its private request and timing settings without disclosing them.
	descriptor, _ := json.Marshal(definition)
	digest := sha256.Sum256(descriptor)
	binding := sha256.New()
	_, _ = binding.Write([]byte("goby-dynamic-subtitle-v1\x00"))
	_, _ = binding.Write([]byte(leaseID))
	_, _ = binding.Write([]byte{0})
	_, _ = binding.Write([]byte(definition.ID))
	_, _ = binding.Write([]byte{0})
	_, _ = binding.Write(digest[:])
	return hex.EncodeToString(binding.Sum(nil))
}

func sourceInfo(info media.Info, leaseID string, definitions []SubtitleDefinition) media.Info {
	info = cloneInfo(info)
	for ordinal, definition := range definitions {
		info.Streams = append(info.Streams, media.Stream{Index: ExternalSubtitleIndexBase + ordinal,
			Codec: definition.Format, CodecType: "subtitle", Language: definition.Language, Title: definition.Name,
			IsDefault: definition.Default, IsForced: definition.Forced, IsExternal: true, IsTextSubtitleStream: true,
			SubtitleTag: subtitleTag(leaseID, definition)})
	}
	return info
}

// Subtitle resolves a private sidecar only after current lease authorization
// and a generation fence. For dynamic inputs SubtitleTag identifies the
// operator declaration, not a full-content digest. It stays stable across a
// lease's reconnects so its fixed HLS track set can retain a continuous window.
// The caller must retain the generation fence when publishing fetched cues.
func (m *Manager) Subtitle(ctx context.Context, owner Owner, leaseID string, generation uint64, streamIndex int, tag string) (SubtitleDefinition, error) {
	if err := ctx.Err(); err != nil {
		return SubtitleDefinition{}, err
	}
	if _, err := m.Info(ctx, owner, leaseID); err != nil {
		return SubtitleDefinition{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.leases[leaseID]
	if m.closing || state == nil || state.closed || state.key.owner != owner.Identity() {
		return SubtitleDefinition{}, ErrNotFound
	}
	if err := ctx.Err(); err != nil {
		return SubtitleDefinition{}, err
	}
	if state.opening {
		return SubtitleDefinition{}, ErrBusy
	}
	if generation == 0 || state.lease.Generation != generation {
		return SubtitleDefinition{}, ErrStaleGeneration
	}
	ordinal := streamIndex - ExternalSubtitleIndexBase
	if ordinal < 0 || ordinal >= len(state.definition.Subtitles) {
		return SubtitleDefinition{}, ErrNotFound
	}
	definition := state.definition.Subtitles[ordinal]
	expected := subtitleTag(leaseID, definition)
	if len(tag) != len(expected) || subtle.ConstantTimeCompare([]byte(tag), []byte(expected)) != 1 {
		return SubtitleDefinition{}, ErrNotFound
	}
	definition.Headers = cloneHeaders(definition.Headers)
	return definition, nil
}

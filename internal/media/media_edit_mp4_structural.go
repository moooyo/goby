package media

import "strconv"

// Layout is private descriptor-bound evidence from the complete structural
// admission scan. It never accepts byte offsets from an HTTP request or a job.
type mediaEditMP4Layout struct {
	SourceBytes   int64
	MovieDuration uint64
	Tracks        []mediaEditMP4TrackLayout
}

type mediaEditMP4TrackLayout struct {
	ID                          uint64
	Codec                       string
	Duration                    uint64
	TypeOffset, BodyOffset, End int64
}

type mediaEditMP4RemovalPlan struct {
	SourceBytes     int64
	RemovedTrackID  uint64
	TrackTypeOffset int64
	TrackBodyOffset int64
	TrackEnd        int64
}

// buildMediaEditMP4RemovalPlan removes an admitted tx3g track declaration
// without relocating any other box or sample. The movie timeline must remain
// exactly valid without rewriting mvhd: removal of its sole longest track is
// rejected. Unreferenced mdat bytes remain; this is not secure payload erasure.
func buildMediaEditMP4RemovalPlan(proof mediaEditContainerProof, document mediaEditDocument, removedIndex int) (mediaEditMP4RemovalPlan, error) {
	fail := func() (mediaEditMP4RemovalPlan, error) {
		return mediaEditMP4RemovalPlan{}, mediaEditContainerError("MP4 structural removal lacks a complete track binding")
	}
	layout := proof.MP4Layout
	if layout == nil || layout.SourceBytes <= 0 || layout.SourceBytes > MaxSubtitleRemovalInputBytes || len(layout.Tracks) < 2 || len(layout.Tracks) > mediaEditMaxStreams || len(layout.Tracks) != len(document.Streams) || len(layout.Tracks) != len(proof.MP4Tracks) {
		return fail()
	}
	byID := make(map[uint64]mediaEditMP4TrackLayout, len(layout.Tracks))
	for index, track := range layout.Tracks {
		headerRemainder := track.BodyOffset - track.TypeOffset
		if track.ID == 0 || track.ID > 0xffffffff || track.TypeOffset < 4 || track.TypeOffset > layout.SourceBytes-4 || track.BodyOffset < 0 || headerRemainder != 4 && headerRemainder != 12 || track.BodyOffset >= track.End || track.End > layout.SourceBytes {
			return fail()
		}
		if _, duplicate := byID[track.ID]; duplicate {
			return fail()
		}
		for _, previous := range layout.Tracks[:index] {
			if track.TypeOffset-4 < previous.End && previous.TypeOffset-4 < track.End {
				return fail()
			}
		}
		byID[track.ID] = track
	}
	seenProofIDs := make(map[uint64]bool, len(proof.MP4Tracks))
	for _, track := range proof.MP4Tracks {
		physical, exists := byID[track.ID]
		if !exists || physical.Codec != track.Codec || seenProofIDs[track.ID] {
			return fail()
		}
		seenProofIDs[track.ID] = true
	}
	seenIndexes := make(map[int64]bool, len(document.Streams))
	seenIDs := make(map[uint64]bool, len(document.Streams))
	var selected mediaEditMP4TrackLayout
	var longestRetained uint64
	for _, stream := range document.Streams {
		index, err := mediaEditInteger(stream["index"])
		if err != nil || index < 0 || index > 4095 || seenIndexes[index] {
			return fail()
		}
		text, valid := stream["id"].(string)
		id, err := strconv.ParseUint(text, 0, 32)
		physical, exists := byID[id]
		if !valid || err != nil || !exists || seenIDs[id] {
			return fail()
		}
		seenIndexes[index], seenIDs[id] = true, true
		if int(index) == removedIndex {
			if physical.Codec != "tx3g" || stream["codec_type"] != "subtitle" || stream["codec_name"] != "mov_text" {
				return fail()
			}
			selected = physical
		} else {
			longestRetained = max(longestRetained, physical.Duration)
		}
	}
	if selected.ID == 0 || longestRetained != layout.MovieDuration {
		return mediaEditMP4RemovalPlan{}, mediaEditContainerError("selected MP4 subtitle cannot be removed while preserving the movie timeline")
	}
	return mediaEditMP4RemovalPlan{SourceBytes: layout.SourceBytes, RemovedTrackID: selected.ID, TrackTypeOffset: selected.TypeOffset,
		TrackBodyOffset: selected.BodyOffset, TrackEnd: selected.End}, nil
}

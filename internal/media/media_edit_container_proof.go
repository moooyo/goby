package media

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
)

// These facts are not all visible in ffprobe's stream/chapter projection.
// Container-local track IDs are used only to bind that projection to its
// structural facts; they are excluded from the normalized preservation digest.
type mediaEditContainerProof struct {
	Writer                  map[string]string
	Chapters                []mediaEditChapterDisplayProof
	MP4Tracks               []mediaEditMP4TrackProof
	MP4Layout               *mediaEditMP4Layout
	MatroskaTracks          []mediaEditMatroskaTrackProof
	MatroskaAttachmentCount int
}

type mediaEditMP4RollProof struct {
	Samples  uint64 `json:"samples"`
	Distance int16  `json:"distance"`
}

type mediaEditMP4TrackProof struct {
	ID       uint64
	Codec    string
	Samples  uint64
	Roll     mediaEditMP4RollProof
	UserData mediaEditMP4UserDataProof
}

type mediaEditMP4TrackSemantics struct {
	Codec    string                    `json:"codec"`
	Samples  uint64                    `json:"samples"`
	Roll     mediaEditMP4RollProof     `json:"roll"`
	UserData mediaEditMP4UserDataProof `json:"user_data"`
}

type mediaEditMP4StreamBinding struct {
	RemovedSourceID  uint64
	SourceID         uint64
	CandidateID      uint64
	SourcePackets    uint64
	CandidatePackets uint64
}

func mediaEditMP4Bindings(source, candidate mediaEditDocument, removedIndex int, pairs []MediaEditStreamEvidence, sourcePackets, candidatePackets map[int]mediaEditPacketDigest) ([]mediaEditMP4StreamBinding, error) {
	streamIDs := func(document mediaEditDocument) (map[int]uint64, error) {
		result := make(map[int]uint64, len(document.Streams))
		seen := make(map[uint64]bool, len(document.Streams))
		for _, stream := range document.Streams {
			index, err := mediaEditInteger(stream["index"])
			if err != nil || index < 0 || index > 4095 {
				return nil, mediaEditContainerError("invalid MP4 probe stream index")
			}
			text, ok := stream["id"].(string)
			if !ok || len(text) > 18 {
				return nil, mediaEditContainerError("MP4 probe did not report its track ID")
			}
			id, err := strconv.ParseUint(text, 0, 32)
			if err != nil || id == 0 || id > math.MaxUint32 || seen[id] {
				return nil, mediaEditContainerError("ambiguous MP4 probe track ID")
			}
			seen[id] = true
			result[int(index)] = id
		}
		return result, nil
	}
	before, err := streamIDs(source)
	if err != nil {
		return nil, err
	}
	after, err := streamIDs(candidate)
	if err != nil {
		return nil, err
	}
	if len(before) != len(pairs)+1 || len(after) != len(pairs) {
		return nil, mediaEditContainerError("MP4 probe inventory differs from the selected removal")
	}
	removedID := before[removedIndex]
	if removedID == 0 {
		return nil, mediaEditContainerError("selected MP4 subtitle has no structural track ID")
	}
	bindings := make([]mediaEditMP4StreamBinding, 0, len(pairs))
	for _, pair := range pairs {
		original, originalOK := sourcePackets[pair.SourceIndex]
		remuxed, remuxedOK := candidatePackets[pair.CandidateIndex]
		if pair.SourceIndex == removedIndex || before[pair.SourceIndex] == 0 || after[pair.CandidateIndex] == 0 || !originalOK || !remuxedOK || original.Packets <= 0 || remuxed.Packets <= 0 {
			return nil, mediaEditContainerError("MP4 track has no complete packet binding")
		}
		bindings = append(bindings, mediaEditMP4StreamBinding{RemovedSourceID: removedID, SourceID: before[pair.SourceIndex], CandidateID: after[pair.CandidateIndex],
			SourcePackets: uint64(original.Packets), CandidatePackets: uint64(remuxed.Packets)})
	}
	return bindings, nil
}

func compareMediaEditContainerProofs(source, candidate mediaEditContainerProof, bindings []mediaEditMP4StreamBinding) (string, error) {
	if !reflect.DeepEqual(source.Chapters, candidate.Chapters) {
		return "", mediaEditContainerError("retained chapter display semantics changed")
	}
	tracks := []mediaEditMP4TrackSemantics{}
	if len(source.MP4Tracks) != 0 || len(candidate.MP4Tracks) != 0 {
		if len(bindings) == 0 || len(source.MP4Tracks) != len(bindings)+1 || len(candidate.MP4Tracks) != len(bindings) {
			return "", mediaEditContainerError("MP4 structural inventory differs from the selected removal")
		}
		byID := func(proofs []mediaEditMP4TrackProof) (map[uint64]mediaEditMP4TrackProof, error) {
			result := make(map[uint64]mediaEditMP4TrackProof, len(proofs))
			for _, proof := range proofs {
				if proof.ID == 0 || proof.ID > math.MaxUint32 || proof.Samples == 0 || proof.Samples > uint64(mediaEditMaxPackets) {
					return nil, mediaEditContainerError("invalid MP4 structural track identity")
				}
				if err := mediaEditMP4ValidateUserDataProof(proof.UserData); err != nil {
					return nil, err
				}
				if proof.Codec == "mp4a" {
					if proof.Roll.Samples != proof.Samples || proof.Roll.Distance != -1 {
						return nil, mediaEditContainerError("incomplete MP4 AAC preroll proof")
					}
				} else if (proof.Codec != "avc1" && proof.Codec != "tx3g") || proof.Roll != (mediaEditMP4RollProof{}) {
					return nil, mediaEditContainerError("unsupported MP4 structural codec or preroll")
				}
				if _, duplicate := result[proof.ID]; duplicate {
					return nil, mediaEditContainerError("duplicate MP4 structural track identity")
				}
				result[proof.ID] = proof
			}
			return result, nil
		}
		before, err := byID(source.MP4Tracks)
		if err != nil {
			return "", err
		}
		after, err := byID(candidate.MP4Tracks)
		if err != nil {
			return "", err
		}
		for _, binding := range bindings {
			original, originalOK := before[binding.SourceID]
			remuxed, remuxedOK := after[binding.CandidateID]
			if binding.RemovedSourceID == 0 || binding.RemovedSourceID != bindings[0].RemovedSourceID || !originalOK || !remuxedOK || original.Samples != binding.SourcePackets || remuxed.Samples != binding.CandidatePackets ||
				original.Codec != remuxed.Codec || original.Samples != remuxed.Samples || original.Roll != remuxed.Roll || !reflect.DeepEqual(original.UserData, remuxed.UserData) {
				return "", mediaEditContainerError("retained MP4 sample-group or packet-binding semantics changed")
			}
			delete(before, binding.SourceID)
			delete(after, binding.CandidateID)
			tracks = append(tracks, mediaEditMP4TrackSemantics{Codec: original.Codec, Samples: original.Samples, Roll: original.Roll, UserData: original.UserData})
		}
		if len(before) != 1 || len(after) != 0 {
			return "", mediaEditContainerError("MP4 retained tracks were not completely bound")
		}
		removed, found := before[bindings[0].RemovedSourceID]
		if !found || removed.Codec != "tx3g" {
			return "", mediaEditContainerError("the removed MP4 track is not the selected subtitle")
		}
	} else if len(bindings) != 0 {
		return "", mediaEditContainerError("MP4 packet binding has no structural evidence")
	}
	digest, err := mediaEditJSONDigest(struct {
		Version  int                            `json:"version"`
		Chapters []mediaEditChapterDisplayProof `json:"chapters"`
		Tracks   []mediaEditMP4TrackSemantics   `json:"tracks"`
	}{mediaEditProofVersion, source.Chapters, tracks})
	if err != nil {
		return "", fmt.Errorf("hash container preservation semantics: %w", err)
	}
	return digest, nil
}

package playback

import (
	"math/big"
	"strconv"
	"strings"
)

type conditionFacts struct {
	source  Source
	streams selectedStreams
}

type factKind uint8

const (
	unknownFact factKind = iota
	numericFact
	textFact
	booleanFact
)

type fact struct {
	kind    factKind
	known   bool
	number  *big.Rat
	text    string
	boolean bool
}

func numberFact(value int64, known bool) fact {
	return fact{kind: numericFact, known: known, number: new(big.Rat).SetInt64(value)}
}

func stringFact(value string, known bool) fact {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "unknown", "n/a", "unspecified":
		known = false
	}
	return fact{kind: textFact, known: known && value != "", text: value}
}

func boolFact(value, known bool) fact {
	return fact{kind: booleanFact, known: known, boolean: value}
}

func (facts conditionFacts) value(property ProfileConditionValue) fact {
	video, audio := facts.streams.video, facts.streams.audio
	switch strings.ToLower(string(property)) {
	case "numaudiostreams":
		return numberFact(int64(facts.streams.audioCount), true)
	case "numvideostreams":
		return numberFact(int64(facts.streams.videoCount), true)
	case "width", "height", "videobitdepth", "videobitrate", "videolevel", "refframes", "videoframerate":
		if video == nil {
			return numberFact(0, false)
		}
		switch strings.ToLower(string(property)) {
		case "width":
			return numberFact(int64(video.Width), video.Width > 0)
		case "height":
			return numberFact(int64(video.Height), video.Height > 0)
		case "videobitdepth":
			return numberFact(int64(video.BitDepth), video.BitDepth > 0)
		case "videobitrate":
			return numberFact(video.Bitrate, video.Bitrate > 0)
		case "videolevel":
			// Preserve the level units supplied by ffprobe and the media DTO.
			// Codec-dependent rescaling requires a separately verified mapping.
			return numberFact(int64(video.Level), video.Level > 0)
		case "refframes":
			return numberFact(int64(video.RefFrames), video.RefFrames > 0)
		case "videoframerate":
			for _, text := range []string{video.AverageFrameRate, video.RealFrameRate} {
				if value, ok := boundedNumber(text); ok && value.Sign() > 0 {
					return fact{kind: numericFact, known: true, number: value}
				}
			}
		}
		return numberFact(0, false)
	case "audiochannels", "audiobitrate", "audiosamplerate", "audiobitdepth":
		if audio == nil {
			return numberFact(0, false)
		}
		switch strings.ToLower(string(property)) {
		case "audiochannels":
			return numberFact(int64(audio.Channels), audio.Channels > 0)
		case "audiobitrate":
			return numberFact(audio.Bitrate, audio.Bitrate > 0)
		case "audiosamplerate":
			return numberFact(int64(audio.SampleRate), audio.SampleRate > 0)
		case "audiobitdepth":
			return numberFact(int64(audio.BitDepth), audio.BitDepth > 0)
		}
	case "videoprofile", "videocodectag", "videorange":
		if video == nil {
			return stringFact("", false)
		}
		switch strings.ToLower(string(property)) {
		case "videoprofile":
			return stringFact(video.Profile, true)
		case "videocodectag":
			return stringFact(video.CodecTag, true)
		case "videorange":
			return stringFact(video.VideoRange, video.VideoRangeKnown)
		}
	case "audioprofile":
		if audio != nil {
			return stringFact(audio.Profile, true)
		}
		return stringFact("", false)
	case "isinterlaced":
		if video != nil {
			return boolFact(video.IsInterlaced, video.InterlaceKnown)
		}
		return boolFact(false, false)
	case "isavc":
		if video != nil {
			return boolFact(video.IsAVC, video.IsAVCKnown)
		}
		return boolFact(false, false)
	case "isexternalaudio":
		if audio != nil {
			return boolFact(audio.IsExternal, true)
		}
		return boolFact(false, false)
	case "has64bitoffsets", "isanamorphic", "issecondaryaudio":
		return boolFact(false, false)
	case "packetlength", "videorotation":
		return numberFact(0, false)
	case "videotimestamp":
		return stringFact("", false)
	}
	return fact{kind: unknownFact}
}

type conditionState uint8

const (
	conditionPass conditionState = iota
	conditionFail
	conditionUnknown
	conditionInvalid
)

func evaluateCondition(condition ProfileCondition, facts conditionFacts) conditionState {
	operator := strings.ToLower(string(condition.Condition))
	switch operator {
	case "equals", "notequals", "lessthanequal", "greaterthanequal", "equalsany":
	default:
		return conditionInvalid
	}
	actual := facts.value(condition.Property)
	if actual.kind == unknownFact {
		return conditionUnknown
	}
	values := []string{strings.TrimSpace(condition.Value)}
	if operator == "equalsany" {
		// Goby accepts comma- or pipe-separated exact alternatives. This input
		// syntax is a local contract, not a claim about unobserved Emby parsing.
		values = strings.FieldsFunc(condition.Value, func(value rune) bool { return value == ',' || value == '|' })
	}
	if len(values) == 0 || len(values) > maxProfileEntries {
		return conditionInvalid
	}
	matched := false
	for _, text := range values {
		text = strings.TrimSpace(text)
		if text == "" {
			return conditionInvalid
		}
		var comparison int
		switch actual.kind {
		case numericFact:
			value, ok := boundedNumber(text)
			if !ok {
				return conditionInvalid
			}
			if actual.known {
				comparison = actual.number.Cmp(value)
			}
		case textFact:
			if operator == "lessthanequal" || operator == "greaterthanequal" {
				return conditionInvalid
			}
			if !strings.EqualFold(actual.text, text) {
				comparison = 1
			}
		case booleanFact:
			if operator == "lessthanequal" || operator == "greaterthanequal" {
				return conditionInvalid
			}
			var expected bool
			switch strings.ToLower(text) {
			case "true":
				expected = true
			case "false":
			default:
				return conditionInvalid
			}
			if actual.boolean != expected {
				comparison = 1
			}
		}
		switch operator {
		case "equals", "equalsany":
			matched = matched || comparison == 0
		case "notequals":
			matched = comparison != 0
		case "lessthanequal":
			matched = comparison <= 0
		case "greaterthanequal":
			matched = comparison >= 0
		}
	}
	if !actual.known {
		return conditionUnknown
	}
	if matched {
		return conditionPass
	}
	return conditionFail
}

func conditionReason(condition ProfileCondition, state conditionState) Reason {
	reason := Reason{Property: string(condition.Property)}
	switch state {
	case conditionUnknown:
		if condition.IsRequired == nil || !*condition.IsRequired {
			reason.Code, reason.Message, reason.Unverified = "unverified_condition", "An optional profile condition could not be verified from source facts.", true
		} else {
			reason.Code, reason.Message = "unknown_required_condition", "A required profile condition could not be verified from source facts."
		}
	case conditionInvalid:
		reason.Code, reason.Message = "invalid_profile_condition", "A profile condition uses an unsupported comparison or malformed value."
	case conditionFail:
		reason.Code, reason.Message = "profile_condition_failed", "The source does not satisfy a declared profile condition."
	}
	return reason
}

func evaluateConditions(conditions []ProfileCondition, facts conditionFacts) []Reason {
	var reasons []Reason
	for _, condition := range conditions {
		state := evaluateCondition(condition, facts)
		if state != conditionPass {
			reasons = append(reasons, conditionReason(condition, state))
		}
	}
	return reasons
}

func applies(conditions []ProfileCondition, facts conditionFacts) (bool, []Reason) {
	var reasons []Reason
	var failed, invalid, unknownRequired bool
	for _, condition := range conditions {
		state := evaluateCondition(condition, facts)
		if state != conditionPass {
			reason := conditionReason(condition, state)
			if state != conditionFail {
				reasons = append(reasons, reason)
			}
			failed = failed || state == conditionFail
			invalid = invalid || state == conditionInvalid
			unknownRequired = unknownRequired || state == conditionUnknown && !reason.Unverified
		}
	}
	if invalid {
		return false, reasons
	}
	if failed {
		// A known false conjunct establishes non-applicability regardless of
		// other unknown facts; the result must not depend on condition order.
		return false, nil
	}
	if unknownRequired {
		return false, reasons
	}
	return true, reasons
}

// Bound both input length and decimal exponents before math/big sees them.
// Rational frame rates remain exact and NaN/Inf or hexadecimal forms are invalid.
func boundedNumber(text string) (*big.Rat, bool) {
	if len(text) == 0 || len(text) > 128 {
		return nil, false
	}
	for _, character := range text {
		if !strings.ContainsRune("0123456789+-.eE/", character) {
			return nil, false
		}
	}
	for _, part := range strings.Split(text, "/") {
		if index := strings.IndexAny(part, "eE"); index >= 0 {
			exponent, err := strconv.Atoi(part[index+1:])
			if err != nil || exponent < -1000 || exponent > 1000 {
				return nil, false
			}
		}
	}
	value, ok := new(big.Rat).SetString(text)
	return value, ok
}

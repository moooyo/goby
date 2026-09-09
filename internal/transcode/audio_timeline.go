package transcode

// AudioTimelineOptions describes the reference packets used by an audio-only
// HLS producer. Codec and SampleRate come from the validated transcode plan.
// Zero SampleRate has the runner's explicit 48 kHz encoding default.
type AudioTimelineOptions struct {
	Codec      string
	SampleRate int
	// Copy requires exact, source-relative packet facts. The last packet must
	// contain effective presentation samples, and its untrimmed duration must
	// cover the source end. A pointer distinguishes a known zero from no fact.
	// The caller must establish continuous audio coverage starting at the
	// source origin; estimated durations and delayed/gapped tracks do not qualify.
	LastPacketStartTicks   *int64
	MaxPacketDurationTicks int64
}

// BuildAudioTimeline preserves the complete, exact presentation duration while
// avoiding a final cut for which no reference packet is guaranteed to exist.
// It never clips the source, creates a replacement URL for an existing segment,
// or changes a published timeline. Build once before publishing the manifest.
//
// Encoding merges a remainder of at most one output frame. It intentionally
// does not claim to know the encoder's exact first packet phase or invent a
// source last-packet timestamp. The closed encoder set uses AAC-LC's 1024-sample
// frames and libmp3lame's MPEG-1 1152 / MPEG-2 and MPEG-2.5 576-sample frames.
//
// Copy retains a cut only when the last usable packet starts at least one
// maximum packet duration after it. This conservative phase allowance covers
// the first retained packet after priming or seeking without pretending it
// starts exactly at zero. A valid one-packet tail can therefore be merged too.
func BuildAudioTimeline(durationTicks int64, segmentSeconds int, options AudioTimelineOptions) (Timeline, error) {
	if durationTicks <= 0 || durationTicks > maxDurationTicks || segmentSeconds < 1 || segmentSeconds > 10 {
		return Timeline{}, ErrInvalidTimeline
	}
	var lastCut int64
	if options.Codec == "copy" {
		if options.LastPacketStartTicks == nil || options.MaxPacketDurationTicks <= 0 || options.MaxPacketDurationTicks > maxDurationTicks {
			return Timeline{}, ErrUnsupportedTimeline
		}
		lastPacket := *options.LastPacketStartTicks
		if lastPacket < 0 || lastPacket >= durationTicks {
			return Timeline{}, ErrUnsupportedTimeline
		}
		// Packet starts are rounded down while presentation ends and packet
		// durations are rounded up. Their difference may exceed the rounded
		// duration by exactly one tick (100 ns). Subtract after validating the
		// ordered endpoints instead of adding to the packet-duration bound.
		if durationTicks-lastPacket-1 > options.MaxPacketDurationTicks {
			return Timeline{}, ErrUnsupportedTimeline
		}
		lastCut = lastPacket - options.MaxPacketDurationTicks
	} else {
		frameTicks, err := audioOutputFrameTicks(options.Codec, options.SampleRate)
		if err != nil {
			return Timeline{}, err
		}
		// Equality also merges: a source ending exactly one rounded frame
		// after a nominal cut does not establish the next packet's phase.
		lastCut = durationTicks - frameTicks - 1
	}
	nominal := int64(segmentSeconds) * ticksPerSecond
	count := max(int64(0), lastCut/nominal)
	if count >= MaxTimelineSegments {
		return Timeline{}, ErrTimelineLimit
	}
	cuts := make([]int64, int(count))
	for index := range cuts {
		cuts[index] = int64(index+1) * nominal
	}
	return timelineFromCuts(durationTicks, cuts), nil
}

func audioOutputFrameTicks(codec string, sampleRate int) (int64, error) {
	if sampleRate == 0 {
		sampleRate = 48000
	}
	switch sampleRate {
	case 8000, 11025, 12000, 16000, 22050, 24000, 32000, 44100, 48000, 64000, 88200, 96000:
	default:
		return 0, ErrUnsupportedTimeline
	}
	var samples int64
	switch codec {
	case "aac":
		samples = 1024
	case "mp3":
		if sampleRate > 48000 {
			return 0, ErrUnsupportedTimeline
		}
		samples = 1152
		if sampleRate <= 24000 {
			samples = 576
		}
	default:
		return 0, ErrUnsupportedTimeline
	}
	return (samples*ticksPerSecond + int64(sampleRate) - 1) / int64(sampleRate), nil
}

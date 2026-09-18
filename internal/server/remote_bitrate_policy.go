package server

import (
	"math"
	"math/big"
	"strings"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

// RemoteClientBitrateLimit is a media planning and admission ceiling in bits
// per second, not a packet shaper or a guarantee about instantaneous bandwidth.
// Application credentials keep their separate server-owned playback authority.
func principalRemoteBitrateLimit(principal identity.Principal) (int64, error) {
	if principal.IsApplicationKey() {
		return 0, nil
	}
	policy, err := identity.ParseRuntimePolicy(principal.User.Policy)
	if err != nil {
		return 0, err
	}
	if identity.IsLocalPeer(principal.PeerIP) {
		return 0, nil
	}
	return int64(policy.RemoteClientBitrateLimit), nil
}

func applyPrincipalRemoteBitrateLimit(limits playback.ConversionLimits, principal identity.Principal) playback.ConversionLimits {
	limit, err := principalRemoteBitrateLimit(principal)
	if err != nil {
		limits.AllowRemux, limits.AllowAudioTranscode, limits.AllowVideoTranscode = false, false, false
		return limits
	}
	if limit > 0 {
		// Preserve the conversion planner's existing default when no server
		// ceiling was configured; a user limit must never increase that default.
		if limits.MaxBitrate == 0 {
			limits.MaxBitrate = 20_000_000
		}
		limits.MaxBitrate = min(limits.MaxBitrate, limit)
	}
	return limits
}

// Retry once with stream copying disabled when conservative source evidence
// rejects a preferred mixed copy/encode candidate. Codec requirements and
// current encoding permissions still belong to the ordinary planner.
func principalConversionDecision(principal identity.Principal, source library.MediaFile, input playback.Source, request playback.Request, limits playback.ConversionLimits) (playback.ConversionDecision, error) {
	planner := playback.PlanVideoConversion
	if strings.EqualFold(input.ItemType, "Audio") {
		planner = playback.PlanAudioConversion
	}
	decision, err := planner(input, request, limits)
	if err != nil || decision.Plan == nil || principalPlanBitrateAllowed(principal, source, *decision.Plan) {
		return decision, err
	}
	disabled := false
	request.AllowVideoStreamCopy, request.AllowAudioStreamCopy = &disabled, &disabled
	decision, err = planner(input, request, limits)
	if err == nil && decision.Plan != nil && !principalPlanBitrateAllowed(principal, source, *decision.Plan) {
		decision.Plan = nil
	}
	return decision, err
}

func remoteBitrateEncodingValues(values map[string]string) map[string]string {
	result := make(map[string]string, len(values)+2)
	for name, value := range values {
		if strings.EqualFold(name, "allowvideostreamcopy") || strings.EqualFold(name, "allowaudiostreamcopy") {
			continue
		}
		result[name] = value
	}
	result["allowvideostreamcopy"], result["allowaudiostreamcopy"] = "false", "false"
	return result
}

func principalOriginalBitrateAllowed(principal identity.Principal, source library.MediaFile) bool {
	limit, err := principalRemoteBitrateLimit(principal)
	if err != nil {
		return false
	}
	if limit == 0 {
		return true
	}
	bitrate, known := remoteSourceBitrate(source)
	return known && bitrate <= limit
}

// remoteSourceBitrate never changes source declarations. Physical source size
// and probed duration provide an independent average, and the highest known
// source total or stream sum wins. Unknown totals cannot admit an original file.
func remoteSourceBitrate(source library.MediaFile) (int64, bool) {
	if source.Item.Media == nil || source.Size < 0 {
		return 0, false
	}
	info := source.Item.Media
	if info.Bitrate < 0 || info.Size < 0 || info.DurationTicks < 0 {
		return 0, false
	}
	bitrate := info.Bitrate
	if size := max(source.Size, info.Size); size > 0 && info.DurationTicks > 0 {
		average, known := remoteCeilProduct(size, 8*media.TicksPerSecond, info.DurationTicks)
		if !known {
			return 0, false
		}
		bitrate = max(bitrate, average)
	}
	if bitrate <= 0 {
		return 0, false
	}
	var streams int64
	for _, stream := range info.Streams {
		if stream.IsExternal || stream.IsAttachedPicture {
			continue
		}
		if stream.Bitrate < 0 || stream.Bitrate > math.MaxInt64-streams {
			return 0, false
		}
		streams += stream.Bitrate
	}
	return max(bitrate, streams), true
}

// Recompute the budget from the executable plan and current source evidence.
// A caller-provided OutputSource projection alone cannot authorize delivery.
// Copied tracks conservatively reserve the complete known source rate; this
// can decline a copy of one small track in a much larger multi-track source.
func principalPlanBitrateAllowed(principal identity.Principal, source library.MediaFile, plan transcode.Plan) bool {
	limit, err := principalRemoteBitrateLimit(principal)
	if err != nil {
		return false
	}
	if limit == 0 {
		return true
	}
	var encoded, copied int64
	hasCopy := false
	for _, selected := range []struct {
		kind, codec string
		index       int
		bitrate     int64
	}{{"video", plan.VideoCodec, plan.VideoStreamIndex, plan.VideoBitrate}, {"audio", plan.AudioCodec, plan.AudioStreamIndex, plan.AudioBitrate}} {
		if selected.index < 0 {
			continue
		}
		if selected.codec == "copy" {
			hasCopy = true
			stream, found := remoteSelectedStream(source, selected.kind, selected.index)
			if !found || stream.Bitrate < 0 || stream.Bitrate > math.MaxInt64-copied {
				return false
			}
			copied += stream.Bitrate
			continue
		}
		bitrate := selected.bitrate
		if selected.codec == "flac" {
			var known bool
			bitrate, known = remoteFLACBitrate(plan)
			if !known {
				return false
			}
		}
		if selected.kind == "audio" && selected.codec == "pcm_s16le" {
			if plan.AudioChannels < 1 || plan.AudioChannels > 256 || plan.AudioSampleRate < 1 || plan.AudioSampleRate > math.MaxInt32 {
				return false
			}
			var known bool
			bitrate, known = remoteCeilProduct(int64(plan.AudioChannels), int64(plan.AudioSampleRate)*16, 1)
			if !known {
				return false
			}
		}
		if selected.codec == "" || bitrate <= 0 || bitrate > math.MaxInt64-encoded {
			return false
		}
		encoded += bitrate
	}
	if plan.HLS.RenditionCount < 0 || plan.HLS.RenditionCount > len(plan.HLS.Renditions) {
		return false
	}
	for index := 0; index < plan.HLS.RenditionCount; index++ {
		if plan.HLS.Renditions[index].VideoBitrate > plan.VideoBitrate {
			return false
		}
	}
	if hasCopy {
		sourceRate, known := remoteSourceBitrate(source)
		if !known {
			return false
		}
		copied = max(copied, sourceRate)
	}
	if copied > math.MaxInt64-encoded || copied+encoded <= 0 {
		return false
	}
	bitrate := copied + encoded
	if plan.OutputMode != "progressive" {
		// Match the HLS planner's transport reservation. Progressive planners
		// express their ceiling as a media payload budget instead.
		var known bool
		bitrate, known = remoteCeilProduct(bitrate, 10, 9)
		if !known {
			return false
		}
	}
	return bitrate <= limit
}

func remoteSelectedStream(source library.MediaFile, kind string, index int) (media.Stream, bool) {
	if source.Item.Media != nil {
		for _, stream := range source.Item.Media.Streams {
			if stream.Index == index && strings.EqualFold(stream.CodecType, kind) && !stream.IsExternal && !stream.IsAttachedPicture {
				return stream, true
			}
		}
	}
	return media.Stream{}, false
}

// Match the progressive planner's FLAC verbatim-frame bound, including the
// final partial frame. A lossy bitrate target cannot describe FLAC output.
func remoteFLACBitrate(plan transcode.Plan) (int64, bool) {
	if plan.OutputMode != "progressive" || plan.AudioChannels < 1 || plan.AudioChannels > 8 ||
		plan.AudioSampleRate < 1 || plan.AudioSampleRate > 96_000 || plan.AudioBitDepth != 16 && plan.AudioBitDepth != 24 || plan.AudioBitrate != 0 {
		return 0, false
	}
	samples, err := transcode.ProgressiveOutputSamples(plan, plan.AudioSampleRate)
	if err != nil || samples <= 0 {
		return 0, false
	}
	bitsPerSample := int64(plan.AudioChannels * plan.AudioBitDepth)
	if plan.AudioChannels == 2 {
		bitsPerSample++
	}
	frameBytes, known := remoteCeilProduct(samples, bitsPerSample, 8)
	if !known {
		return 0, false
	}
	frames := samples / 4096
	if samples%4096 != 0 {
		frames++
	}
	headerBytes, known := remoteCeilProduct(frames, int64(18+plan.AudioChannels*((plan.AudioBitDepth+14)/8)), 1)
	if !known || frameBytes > math.MaxInt64-headerBytes {
		return 0, false
	}
	return remoteCeilProduct(frameBytes+headerBytes, 8*int64(plan.AudioSampleRate), samples)
}

func remoteCeilProduct(first, second, divisor int64) (int64, bool) {
	if first < 0 || second < 0 || divisor <= 0 {
		return 0, false
	}
	value := new(big.Int).Mul(big.NewInt(first), big.NewInt(second))
	value.Add(value, big.NewInt(divisor-1))
	value.Quo(value, big.NewInt(divisor))
	return value.Int64(), value.IsInt64()
}

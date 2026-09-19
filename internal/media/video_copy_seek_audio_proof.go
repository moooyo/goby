package media

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func parseVideoCopySeekAudioProof(input io.Reader, candidate VideoCopySeekCandidate) error {
	if input == nil || candidate.Audio == nil {
		return fmt.Errorf("video copy seek audio proof is unavailable")
	}
	if err := validateVideoCopySeekCandidate(candidate); err != nil {
		return err
	}
	audio := candidate.Audio
	streams := []videoSeekHashStream{{seen: make(map[string]bool)}}
	headers := make(map[string]bool)
	bounded := &io.LimitedReader{R: input, N: maxVideoCopySeekProofBytes + 1}
	reader := bufio.NewReaderSize(bounded, maxVideoSeekLine)
	seenPacket := false
	for {
		raw, err := reader.ReadSlice('\n')
		if err == io.EOF && len(raw) == 0 {
			break
		}
		if err != nil || len(raw) > maxVideoSeekLine || bounded.N <= 0 {
			return fmt.Errorf("video copy seek audio proof is truncated or exceeds its byte budget")
		}
		line := strings.TrimSpace(string(raw))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if seenPacket {
				return fmt.Errorf("video copy seek audio proof changed its headers")
			}
			if err := parseVideoSeekHashHeader(line, streams, headers); err != nil {
				return err
			}
			continue
		}
		if !headers["format"] || !headers["version"] || !headers["hash"] || len(streams[0].seen) != 3 ||
			streams[0].media != "audio" || streams[0].codec != audio.Codec || streams[0].timeBase == nil ||
			streams[0].timeBase.Cmp(videoSeekTimeBase(audio.TimeBaseNumerator, audio.TimeBaseDenominator)) != 0 {
			return fmt.Errorf("video copy seek audio metadata differs from the source")
		}
		number, record, err := parseVideoSeekHashRecord(line)
		if err != nil || number != 0 || seenPacket {
			return fmt.Errorf("video copy seek audio proof has unexpected packet records")
		}
		duration, durationErr := strconv.ParseInt(strings.TrimSpace(strings.Split(line, ",")[3]), 10, 64)
		timestamp, timestampErr := videoCopySeekOutputTimestamp(candidate, streams[0].timeBase)
		if durationErr != nil || duration != audio.Duration || timestampErr != nil || record.pts != timestamp || record.dts != timestamp ||
			len(record.sideData) != 0 || record.hash != audio.PacketSHA256 {
			return fmt.Errorf("video copy seek audio packet does not match its indexed boundary")
		}
		seenPacket = true
	}
	if !seenPacket {
		return fmt.Errorf("video copy seek audio proof has no packet")
	}
	return nil
}

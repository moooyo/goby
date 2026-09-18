//go:build linux

package transcode

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
)

const (
	maxPackedSegmentBytes = 64 << 20
	maxPackedAudioFrames  = 65_536
	packedTimestampOwner  = "com.apple.streaming.transportStreamTimestamp"
)

// packedHLSPublisher adds the transport timestamp required by packed-audio HLS.
// A subsequent private segment proves that FFmpeg closed the preceding one;
// only successful process completion permits publication of the final segment.
type packedHLSPublisher struct {
	directory string
	plan      Plan
	published []MediaSegment
	lastList  string
	err       error
}

func (p *packedHLSPublisher) publish(finished bool) (publishErr error) {
	if p.err != nil {
		return p.err
	}
	defer func() {
		if publishErr != nil {
			p.err = publishErr
		}
	}()
	extension := p.plan.AudioCodec
	if p.plan.HLS.SegmentType != "packed" || (extension != "aac" && extension != "mp3") ||
		p.plan.StartTicks < 0 || p.plan.StartTicks > maxDurationTicks ||
		p.plan.SegmentStartNumber < 0 || p.plan.SegmentStartNumber >= MaxPlaylistSegments {
		return ErrInvalidPlan
	}
	fd, err := syscall.Open(p.directory, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return cacheOpenError(err)
	}
	dir := os.NewFile(uintptr(fd), p.directory)
	defer dir.Close()
	file, err := cacheOpenRegular(dir, "segment-list.m3u8", syscall.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) && !finished && len(p.published) == 0 {
		return nil
	}
	if err != nil {
		return err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, MaxPlaylistBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return errors.Join(readErr, closeErr)
	}
	if len(data) > MaxPlaylistBytes {
		return ErrInvalidPlaylist
	}
	if !finished && !bytes.Contains(data, []byte("#EXTINF:")) {
		if len(p.published) != 0 {
			return ErrInvalidPlaylist
		}
		return nil
	}
	list, err := parsePrivatePackedList(data, p.plan.SegmentStartNumber, extension)
	if err != nil {
		return err
	}
	if finished && !list.Ended || len(list.Segments) < len(p.published) ||
		len(list.Segments) > MaxPlaylistSegments-p.plan.SegmentStartNumber {
		return ErrInvalidPlaylist
	}
	var elapsed int64
	for index, segment := range list.Segments {
		if segment.Discontinuity || segment.DurationTicks > maxDurationTicks-elapsed ||
			index < len(p.published) && segment != p.published[index] {
			return ErrInvalidPlaylist
		}
		elapsed += segment.DurationTicks
	}
	timestampTicks := p.plan.StartTicks
	for index, segment := range list.Segments {
		if index < len(p.published) {
			timestampTicks += segment.DurationTicks
			continue
		}
		if !finished {
			next := fmt.Sprintf("segment-%06d.%s.tmp", segment.Number+1, extension)
			nextFile, nextErr := cacheOpenRegular(dir, next, syscall.O_RDONLY, 0)
			if errors.Is(nextErr, os.ErrNotExist) {
				break
			}
			if nextErr != nil {
				return nextErr
			}
			if err := nextFile.Close(); err != nil {
				return err
			}
		}
		if err := publishPackedSegment(dir, segment.Name, extension, timestampTicks); err != nil {
			return err
		}
		p.published = append(p.published, segment)
		timestampTicks += segment.DurationTicks
	}
	if len(p.published) == 0 {
		return nil
	}
	var output strings.Builder
	fmt.Fprintf(&output, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXT-X-TARGETDURATION:%d\n#EXT-X-PLAYLIST-TYPE:EVENT\n", p.plan.SegmentStartNumber, list.TargetDuration)
	for _, segment := range p.published {
		fmt.Fprintf(&output, "#EXTINF:%s,\n%s\n", tickSeconds(segment.DurationTicks), segment.Name)
	}
	if finished && list.Ended && len(p.published) == len(list.Segments) {
		output.WriteString("#EXT-X-ENDLIST\n")
	}
	if output.Len() > MaxPlaylistBytes {
		return ErrInvalidPlaylist
	}
	if output.String() == p.lastList {
		return nil
	}
	if err := publishPackedManifest(dir, output.String()); err != nil {
		return err
	}
	p.lastList = output.String()
	return nil
}

func parsePrivatePackedList(data []byte, sequence int, extension string) (MediaPlaylist, error) {
	if len(data) > MaxPlaylistBytes || (extension != "aac" && extension != "mp3") {
		return MediaPlaylist{}, ErrInvalidPlaylist
	}
	var normalized strings.Builder
	seenCache := false
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if line == "#EXT-X-ALLOW-CACHE:YES" {
			if seenCache {
				return MediaPlaylist{}, ErrInvalidPlaylist
			}
			seenCache = true
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:") {
			value := strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:")
			number, err := strconv.ParseInt(value, 10, 32)
			if err != nil || number < 0 || strconv.FormatInt(number, 10) != value {
				return MediaPlaylist{}, ErrInvalidPlaylist
			}
			line = "#EXT-X-MEDIA-SEQUENCE:" + strconv.Itoa(sequence)
		}
		if line != "" && !strings.HasPrefix(line, "#") {
			if !strings.HasSuffix(line, "."+extension+".tmp") {
				return MediaPlaylist{}, ErrInvalidPlaylist
			}
			line = strings.TrimSuffix(line, ".tmp")
			number, rendition, mediaExtension, ok := generatedHLSSegment(line)
			if !ok || rendition != "" || mediaExtension != extension || line != fmt.Sprintf("segment-%06d.%s", number, extension) {
				return MediaPlaylist{}, ErrInvalidPlaylist
			}
		}
		normalized.WriteString(line)
		normalized.WriteByte('\n')
	}
	list, err := ParseMediaPlaylist([]byte(normalized.String()))
	if err != nil || list.Independent || list.InitName != "" {
		return MediaPlaylist{}, ErrInvalidPlaylist
	}
	return list, nil
}

func publishPackedSegment(dir *os.File, name, extension string, timestampTicks int64) error {
	source, err := cacheOpenRegular(dir, name+".tmp", syscall.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	if info.Size() <= 0 || info.Size() > maxPackedSegmentBytes {
		return ErrProcess
	}
	if err := validatePackedAudio(source, info.Size(), extension); err != nil {
		return err
	}
	if existing, err := cacheOpenRegular(dir, name, syscall.O_RDONLY, 0); err == nil {
		_ = existing.Close()
		return ErrCacheUnsafe
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	stageName := name + ".publish.tmp"
	publication, err := cacheOpenRegular(dir, stageName, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer syscall.Unlinkat(int(dir.Fd()), stageName)
	_, writeErr := publication.Write(packedTimestampTag(timestampTicks))
	if writeErr == nil {
		_, writeErr = io.CopyN(publication, source, info.Size())
	}
	if writeErr == nil {
		after, statErr := source.Stat()
		if statErr != nil {
			writeErr = statErr
		} else if after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
			writeErr = ErrProcess
		}
	}
	if writeErr == nil {
		writeErr = publication.Sync()
	}
	closeErr := publication.Close()
	if writeErr != nil || closeErr != nil {
		return errors.Join(writeErr, closeErr)
	}
	if err := syscall.Renameat(int(dir.Fd()), stageName, int(dir.Fd()), name); err != nil {
		return err
	}
	return syscall.Unlinkat(int(dir.Fd()), name+".tmp")
}

func publishPackedManifest(dir *os.File, content string) error {
	const stageName = "main.m3u8.publish.tmp"
	publication, err := cacheOpenRegular(dir, stageName, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer syscall.Unlinkat(int(dir.Fd()), stageName)
	_, writeErr := io.WriteString(publication, content)
	if writeErr == nil {
		writeErr = publication.Sync()
	}
	closeErr := publication.Close()
	if writeErr != nil || closeErr != nil {
		return errors.Join(writeErr, closeErr)
	}
	return syscall.Renameat(int(dir.Fd()), stageName, int(dir.Fd()), "main.m3u8")
}

func packedTimestampTag(ticks int64) []byte {
	// Both ID3v2.4 sizes fit in one synchsafe byte for this fixed PRIV frame.
	frameBytes := len(packedTimestampOwner) + 1 + 8
	tag := make([]byte, 10+10+frameBytes)
	copy(tag, "ID3")
	tag[3], tag[9] = 4, byte(10+frameBytes)
	copy(tag[10:], "PRIV")
	tag[17] = byte(frameBytes)
	copy(tag[20:], packedTimestampOwner)
	// Split seconds and fractions before conversion to avoid multiplication
	// overflow, then apply MPEG's 33-bit wrap to the unsigned transport clock.
	timestamp := uint64(ticks/ticksPerSecond)*90_000 + uint64(ticks%ticksPerSecond)*90_000/uint64(ticksPerSecond)
	binary.BigEndian.PutUint64(tag[len(tag)-8:], timestamp&((1<<33)-1))
	return tag
}

func validatePackedAudio(file *os.File, size int64, extension string) error {
	var offset int64
	for tags := 0; ; tags++ {
		var header [10]byte
		n, err := file.ReadAt(header[:], offset)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if n < 3 || string(header[:3]) != "ID3" {
			break
		}
		if tags >= 4 || n != len(header) || header[3] < 2 || header[3] > 4 || header[4] == 0xff ||
			header[6]&0x80 != 0 || header[7]&0x80 != 0 || header[8]&0x80 != 0 || header[9]&0x80 != 0 {
			return ErrProcess
		}
		tagSize := int64(header[6])<<21 | int64(header[7])<<14 | int64(header[8])<<7 | int64(header[9])
		if header[3] == 4 && header[5]&0x10 != 0 {
			tagSize += 10
		}
		offset += 10 + tagSize
		if offset > 1<<20 || offset >= size {
			return ErrProcess
		}
	}
	frames := 0
	for offset < size {
		if frames >= maxPackedAudioFrames {
			return ErrProcess
		}
		var header [7]byte
		headerBytes := 7
		if extension == "mp3" {
			headerBytes = 4
		}
		if _, err := file.ReadAt(header[:headerBytes], offset); err != nil {
			return err
		}
		frameBytes := packedAudioFrameSize(header, extension)
		if frameBytes <= headerBytes || int64(frameBytes) > size-offset {
			return ErrProcess
		}
		offset += int64(frameBytes)
		frames++
	}
	if frames == 0 {
		return ErrProcess
	}
	return nil
}

func packedAudioFrameSize(header [7]byte, extension string) int {
	if header[0] != 0xff {
		return 0
	}
	if extension == "aac" {
		if header[1]&0xf6 != 0xf0 || (header[2]>>2)&0x0f > 12 {
			return 0
		}
		length := int(header[3]&3)<<11 | int(header[4])<<3 | int(header[5]>>5)
		if header[1]&1 == 0 && length <= 9 {
			return 0
		}
		return length
	}
	if extension != "mp3" || header[1]&0xe0 != 0xe0 || (header[1]>>1)&3 != 1 || header[3]&3 == 2 {
		return 0
	}
	version, sampleIndex, rateIndex := (header[1]>>3)&3, (header[2]>>2)&3, header[2]>>4
	if version == 1 || sampleIndex == 3 || rateIndex == 0 || rateIndex == 15 {
		return 0
	}
	sampleRates := [3]int{44100, 48000, 32000}
	bitrates := [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	rate, coefficient := sampleRates[sampleIndex], 144_000
	if version != 3 {
		bitrates = [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
		rate, coefficient = rate/2, 72_000
		if version == 0 {
			rate /= 2
		}
	}
	return coefficient*bitrates[rateIndex]/rate + int((header[2]>>1)&1)
}

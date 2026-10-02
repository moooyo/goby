//go:build linux

package transcode

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

type generatedAVSourceMediaBuffer struct{ bytes.Buffer }

func (buffer *generatedAVSourceMediaBuffer) Write(data []byte) (int, error) {
	if len(data) > (16<<20)-buffer.Len() {
		return 0, ErrTimelineLimit
	}
	return buffer.Buffer.Write(data)
}

// Fixture generation and calibration use the global trusted foreground budget
// and retain the exited leader until its entire process group is retired.
func generatedAVSourceMediaCommand(t *testing.T, ctx context.Context, executable string, args ...string) []byte {
	t.Helper()
	command := exec.CommandContext(ctx, executable, args...)
	command.Env = processEnvironment()
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = time.Second
	var stdout, stderr generatedAVSourceMediaBuffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	var groupMu sync.Mutex
	retired := false
	command.Cancel = func() error {
		groupMu.Lock()
		defer groupMu.Unlock()
		if retired {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	runErr := media.RunProcessWithRetirement(ctx, command, func() error {
		waitErr := waitWithoutReaping(command.Process.Pid)
		groupMu.Lock()
		defer groupMu.Unlock()
		retired = true
		if waitErr != nil {
			return waitErr
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	})
	if runErr != nil || stderr.Len() != 0 {
		t.Fatalf("controlled media command failed: %v stderr=%s", runErr, stderr.String())
	}
	return stdout.Bytes()
}

func generatedAVSourceMediaFixture(t *testing.T, ctx context.Context, ffmpeg string, seconds, channels int, extraVideo bool, rate int, bframes int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.mp4")
	args := []string{"-hide_banner", "-nostdin", "-v", "error", "-y", "-f", "lavfi", "-i", fmt.Sprintf("color=c=blue:s=160x96:r=24:d=%d", seconds),
		"-f", "lavfi", "-i", fmt.Sprintf("aevalsrc=0.15*sin(2*PI*(173*t+127*t*t)):s=%d:d=%d", rate, seconds), "-map", "0:v:0"}
	if extraVideo {
		args = append(args, "-map", "0:v:0")
	}
	args = append(args, "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-bf", strconv.Itoa(bframes), "-g", "24", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-profile:a", "aac_low", "-threads:a", "1", "-ar", strconv.Itoa(rate), "-ac", strconv.Itoa(channels), "-video_track_timescale", "24000", "-movie_timescale", "48000", path)
	generatedAVSourceMediaCommand(t, ctx, ffmpeg, args...)
	generatedAVSourceMediaLogTrackHeaders(t, ctx, path)
	return path
}

func generatedAVSourceMediaLogTrackHeaders(t *testing.T, ctx context.Context, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil || info.Size() <= 0 || info.Size() > 16<<20 {
		t.Fatalf("controlled source diagnostic extent is unsupported: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p := generatedMP4EndpointParser{ctx: ctx}
	index := 0
	if err := p.walk(data, func(top generatedMP4Box) error {
		if top.kind != "moov" {
			return nil
		}
		return p.walk(top.body, func(child generatedMP4Box) error {
			if child.kind != "trak" {
				return nil
			}
			var header []byte
			kind := ""
			if err := p.walk(child.body, func(track generatedMP4Box) error {
				if track.kind == "tkhd" {
					header = track.body
				}
				if track.kind == "mdia" {
					return p.walk(track.body, func(media generatedMP4Box) error {
						if media.kind == "hdlr" && len(media.body) >= 12 {
							kind = string(media.body[8:12])
						}
						return nil
					})
				}
				return nil
			}); err != nil {
				return err
			}
			t.Logf("actual source track index=%d handler=%s tkhd_bytes=%d tkhd_hex=%x", index, kind, len(header), header)
			index++
			return nil
		})
	}); err != nil {
		t.Fatalf("controlled source header observation failed: %v", err)
	}
}

// Rewrite only timing boxes of an actual ordinary MP4 whose mdat precedes moov.
// The coded payload and sample offsets stay fixed. FFprobe subsequently checks
// the resulting real demux clocks; the rewrite supplies no source EOF evidence.
func generatedAVSourceMediaShiftSelected(t *testing.T, source []byte, selected map[int]bool, seconds int64) []byte {
	t.Helper()
	p := generatedMP4EndpointParser{ctx: context.Background()}
	var output []byte
	moovSeen := false
	mdatSeen := false
	if err := p.walk(source, func(top generatedMP4Box) error {
		if top.kind == "mdat" {
			mdatSeen = true
		}
		if top.kind != "moov" {
			output = append(output, generatedEndpointTestBox(top.kind, top.body)...)
			return nil
		}
		if !mdatSeen {
			return errors.New("controlled timing rewrite requires mdat before moov")
		}
		moovSeen = true
		var movieBody []byte
		index := 0
		var movieScale int64
		if err := p.walk(top.body, func(child generatedMP4Box) error {
			body := bytes.Clone(child.body)
			if child.kind == "mvhd" {
				if len(body) != 100 || body[0] != 0 {
					return errors.New("controlled rewrite requires version-zero mvhd")
				}
				movieScale = int64(binary.BigEndian.Uint32(body[12:16]))
				duration := binary.BigEndian.Uint32(body[16:20])
				binary.BigEndian.PutUint32(body[16:20], duration+uint32(seconds*movieScale))
			} else if child.kind == "trak" {
				if selected[index] {
					if movieScale == 0 {
						return errors.New("movie clock must precede controlled track rewrite")
					}
					var trackBody []byte
					if err := p.walk(body, func(trackChild generatedMP4Box) error {
						trackData := bytes.Clone(trackChild.body)
						if trackChild.kind == "tkhd" {
							if len(trackData) != 84 || trackData[0] != 0 {
								return errors.New("controlled rewrite requires version-zero tkhd")
							}
							duration := binary.BigEndian.Uint32(trackData[20:24])
							binary.BigEndian.PutUint32(trackData[20:24], duration+uint32(seconds*movieScale))
						} else if trackChild.kind == "edts" {
							var edits []byte
							if err := p.walk(trackData, func(edit generatedMP4Box) error {
								if edit.kind != "elst" || len(edit.body) != 20 || binary.BigEndian.Uint32(edit.body[:4]) != 0 || binary.BigEndian.Uint32(edit.body[4:8]) != 1 {
									return errors.New("controlled rewrite requires one version-zero rate-one media edit")
								}
								entry := generatedEndpointTestU32(0, 2, uint32(seconds*movieScale), 0xffffffff, 0x10000)
								entry = append(entry, edit.body[8:]...)
								edits = append(edits, generatedEndpointTestBox("elst", entry)...)
								return nil
							}); err != nil {
								return err
							}
							trackData = edits
						}
						trackBody = append(trackBody, generatedEndpointTestBox(trackChild.kind, trackData)...)
						return nil
					}); err != nil {
						return err
					}
					body = trackBody
				}
				index++
			}
			movieBody = append(movieBody, generatedEndpointTestBox(child.kind, body)...)
			return nil
		}); err != nil {
			return err
		}
		output = append(output, generatedEndpointTestBox("moov", movieBody)...)
		return nil
	}); err != nil {
		t.Fatalf("controlled actual edit rewrite failed: %v", err)
	}
	if !moovSeen {
		t.Fatal("actual source had no movie")
	}
	return output
}

type generatedAVSourceDecodedAudio struct {
	samples    int64
	first, end *big.Rat
	contiguous bool
}

func generatedAVSourceMediaDecodedAudio(t *testing.T, ctx context.Context, ffprobe, path string, audioIndex int) generatedAVSourceDecodedAudio {
	t.Helper()
	return generatedAVSourceMediaDecodedAudioMode(t, ctx, ffprobe, path, audioIndex, false)
}

func generatedAVSourceMediaDecodedAudioMode(t *testing.T, ctx context.Context, ffprobe, path string, audioIndex int, strict bool) generatedAVSourceDecodedAudio {
	t.Helper()
	args := []string{"-v", "error", "-threads", "1"}
	if strict {
		args = append(args, "-fflags", "+nofillin-genpts", "-err_detect", "crccheck+bitstream+buffer+explode")
	}
	args = append(args, "-select_streams", strconv.Itoa(audioIndex), "-show_frames", "-show_streams", "-show_entries",
		"frame=media_type,stream_index,pts,nb_samples:frame_side_data=:stream=index,codec_type,time_base,sample_rate:stream_tags=:stream_disposition=:stream_side_data=", "-of", "json", path)
	data := generatedAVSourceMediaCommand(t, ctx, ffprobe, args...)
	var document struct {
		Frames []struct {
			Kind    string `json:"media_type"`
			Index   *int64 `json:"stream_index"`
			PTS     *int64 `json:"pts"`
			Samples *int64 `json:"nb_samples"`
		} `json:"frames"`
		Streams []struct {
			Index    *int64 `json:"index"`
			Kind     string `json:"codec_type"`
			TimeBase string `json:"time_base"`
			Rate     string `json:"sample_rate"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(data, &document); err != nil || len(document.Streams) != 1 || len(document.Frames) == 0 || len(document.Frames) > 8192 {
		t.Fatalf("actual decoded audio lacked bounded records: %v", err)
	}
	stream := document.Streams[0]
	base, err := generatedBoundsTimeBase(stream.TimeBase)
	if err != nil || stream.Index == nil || *stream.Index != int64(audioIndex) || stream.Kind != "audio" || stream.Rate != "48000" {
		t.Fatalf("actual decoded audio clock differed: %+v %v", stream, err)
	}
	result := generatedAVSourceDecodedAudio{contiguous: true}
	for _, frame := range document.Frames {
		if frame.Kind != "audio" || frame.Index == nil || *frame.Index != int64(audioIndex) || frame.PTS == nil || frame.Samples == nil || *frame.Samples < 1 || *frame.Samples > 1024 {
			t.Fatal("actual audio frame lost its source clock or decoded sample count")
		}
		pts := generatedClockSeconds(*frame.PTS, base.Num, base.Den)
		if result.first == nil {
			result.first = new(big.Rat).Set(pts)
		} else if result.end.Cmp(pts) != 0 {
			result.contiguous = false
		}
		result.end = new(big.Rat).Add(pts, new(big.Rat).SetFrac64(*frame.Samples, 48000))
		result.samples += *frame.Samples
	}
	return result
}

func TestGeneratedAVSourceActualNativeAACMetadataCandidate(t *testing.T) {
	ctx, ffmpeg, ffprobe := generatedWindowMediaTools(t)
	for _, channels := range []int{1, 2} {
		t.Run(fmt.Sprintf("%dch", channels), func(t *testing.T) {
			path := generatedAVSourceMediaFixture(t, ctx, ffmpeg, 24, channels, false, 48000, 0)
			source, before := generatedClosureMediaOpenSource(t, path)
			got, err := MeasureGeneratedMP4AVSourceEndpoint(ctx, ffprobe, source, 0, 1)
			if err != nil {
				t.Fatalf("controlled native AVC/AAC did not produce metadata candidate: %v", err)
			}
			if got.SourceIdentity == "" || got.DemuxOrigin != (GeneratedRational{Den: 1}) || got.PresentationEpoch != (GeneratedRational{Den: 1}) || got.DurationTicks != 24*ticksPerSecond ||
				got.Video.SampleCount != 576 || got.Audio.HeadTrimSamples != 1024 || got.Audio.TailTrimSamples != 0 || got.Audio.EffectiveSamples != 1152000 || got.Audio.CodedSamples != 1153024 ||
				got.Audio.Channels != channels || !got.Audio.RollGroupPresent || got.Audio.RollDistance != -1 || got.Audio.RollMappedSamples != got.Audio.SampleCount {
				t.Fatalf("actual native metadata lost edits, groups or full blocks: %+v", got)
			}
			decoded := generatedAVSourceMediaDecodedAudio(t, ctx, ffprobe, path, 1)
			if !decoded.contiguous || decoded.samples != got.Audio.EffectiveSamples || decoded.first.Cmp(generatedAVSourceRat(got.Audio.EffectiveFirst)) != 0 || decoded.end.Cmp(generatedAVSourceRat(got.Audio.EffectiveEnd)) != 0 {
				t.Fatalf("controlled decoded AAC and candidate differ: samples=%d first=%s end=%s certificate=%+v", decoded.samples, decoded.first, decoded.end, got)
			}
			if !transcodeSourceUnchanged(source, before) || ValidateGeneratedMP4AVSourceEndpointIdentity(source, got) != nil {
				t.Fatal("actual source changed across independent evidence")
			}
			if offset, err := source.Seek(0, 1); err != nil || offset != 7 {
				t.Fatalf("candidate changed descriptor offset: %d %v", offset, err)
			}
			t.Logf("metadata candidate with separately observed decoded audio: F=%+v P=%+v samples=%d blocks=%d", got.DemuxOrigin, got.PresentationEpoch, decoded.samples, got.Audio.SampleCount)
		})
	}
}

func TestGeneratedAVSourceActualUnselectedEarlierTrackKeepsFSeparateFromP(t *testing.T) {
	ctx, ffmpeg, ffprobe := generatedWindowMediaTools(t)
	path := generatedAVSourceMediaFixture(t, ctx, ffmpeg, 2, 2, true, 48000, 0)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = generatedAVSourceMediaShiftSelected(t, data, map[int]bool{1: true, 2: true}, 2)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	source, before := generatedClosureMediaOpenSource(t, path)
	got, err := MeasureGeneratedMP4AVSourceEndpoint(ctx, ffprobe, source, 1, 2)
	if err != nil || got.DemuxOrigin != (GeneratedRational{Den: 1}) || got.PresentationEpoch != (GeneratedRational{Num: 2, Den: 1}) || got.Video.EffectiveEnd != (GeneratedRational{Num: 4, Den: 1}) || got.DurationTicks != 2*ticksPerSecond {
		t.Fatalf("actual earlier unselected track collapsed F into P: %+v %v", got, err)
	}
	decoded := generatedAVSourceMediaDecodedAudio(t, ctx, ffprobe, path, 2)
	if decoded.first.Cmp(new(big.Rat).SetInt64(2)) != 0 {
		t.Fatalf("actual selected AAC did not start at P=2: %s", decoded.first)
	}
	if !transcodeSourceUnchanged(source, before) || ValidateGeneratedMP4AVSourceEndpointIdentity(source, got) != nil {
		t.Fatal("source changed across distinct F/P evidence")
	}
	t.Logf("actual independent demux F=%+v selected P=%+v; decoded AAC end=%s metadata end=%+v (candidate only)", got.DemuxOrigin, got.PresentationEpoch, decoded.end, got.Audio.EffectiveEnd)
}

func TestGeneratedAVSourceActualShortFinalAACBlockDoesNotProveEffectiveDecode(t *testing.T) {
	ctx, ffmpeg, ffprobe := generatedWindowMediaTools(t)
	path := generatedAVSourceMediaFixture(t, ctx, ffmpeg, 6, 2, false, 48000, 0)
	source, before := generatedClosureMediaOpenSource(t, path)
	got, err := MeasureGeneratedMP4AVSourceEndpoint(ctx, ffprobe, source, 0, 1)
	if err != nil || got.Audio.EffectiveSamples != 288000 || got.Audio.TailTrimSamples != 768 || got.Audio.CodedSamples != 289792 || got.Audio.TableDurationUnits != 289024 {
		t.Fatalf("short final native AAC metadata changed its full coded block interpretation: %+v %v", got, err)
	}
	// Default demux can apply the final duration trim. That observed effective
	// output cannot erase the distinct complete block capacity in metadata.
	decodedDefault := generatedAVSourceMediaDecodedAudio(t, ctx, ffprobe, path, 1)
	if !decodedDefault.contiguous || decodedDefault.samples != got.Audio.EffectiveSamples ||
		decodedDefault.first.Cmp(generatedAVSourceRat(got.Audio.EffectiveFirst)) != 0 || decodedDefault.end.Cmp(generatedAVSourceRat(got.Audio.EffectiveEnd)) != 0 {
		t.Fatalf("default short-final decode changed its observed effective trim: samples=%d first=%s end=%s candidate=%+v", decodedDefault.samples, decodedDefault.first, decodedDefault.end, got)
	}
	// Probe mode is explicit. Observe its real sample count rather than assuming
	// every MP4 or every FFprobe mode exposes the metadata tail as decoded data.
	decodedStrict := generatedAVSourceMediaDecodedAudioMode(t, ctx, ffprobe, path, 1, true)
	postHeadCapacity := got.Audio.CodedSamples - got.Audio.HeadTrimSamples
	strictExpectedEnd := new(big.Rat).Add(generatedAVSourceRat(got.Audio.EffectiveFirst), new(big.Rat).SetFrac64(decodedStrict.samples, got.Audio.SampleRate))
	if !decodedStrict.contiguous || decodedStrict.first.Cmp(generatedAVSourceRat(got.Audio.EffectiveFirst)) != 0 ||
		decodedStrict.samples < got.Audio.EffectiveSamples || decodedStrict.samples > postHeadCapacity || decodedStrict.end.Cmp(strictExpectedEnd) != 0 {
		t.Fatalf("strict short-final observation escaped its declared block envelope: samples=%d first=%s end=%s candidate=%+v", decodedStrict.samples, decodedStrict.first, decodedStrict.end, got)
	}
	strictExcess := decodedStrict.samples - got.Audio.EffectiveSamples
	strictRelation := "trimmed_to_metadata"
	if strictExcess > 0 {
		strictRelation = "decoded_beyond_metadata"
	}
	if !transcodeSourceUnchanged(source, before) || ValidateGeneratedMP4AVSourceEndpointIdentity(source, got) != nil {
		t.Fatal("source changed across short-final calibration")
	}
	t.Logf("short-final metadata only: coded_samples=%d table_duration=%d head_trim=%d effective_samples=%d tail_trim=%d qualified=false",
		got.Audio.CodedSamples, got.Audio.TableDurationUnits, got.Audio.HeadTrimSamples, got.Audio.EffectiveSamples, got.Audio.TailTrimSamples)
	t.Logf("short-final mode=default decoded_samples=%d first=%s end=%s relation=trimmed_to_metadata observed_excess_samples=0 qualified=false",
		decodedDefault.samples, decodedDefault.first, decodedDefault.end)
	t.Logf("short-final mode=strict ff_flags=+nofillin-genpts decoded_samples=%d first=%s end=%s relation=%s observed_excess_samples=%d qualified=false",
		decodedStrict.samples, decodedStrict.first, decodedStrict.end, strictRelation, strictExcess)
	if strictExcess > 0 {
		t.Logf("short-final strict observation retains %d samples in the declared metadata tail envelope; padding content and publication remain unproved", strictExcess)
	}
}

func TestGeneratedAVSourceActualUnsupportedTracksFailClosed(t *testing.T) {
	ctx, ffmpeg, ffprobe := generatedWindowMediaTools(t)
	for _, test := range []struct {
		name          string
		rate, bframes int
	}{{"resampled clock", 44100, 0}, {"reordered AVC", 48000, 2}} {
		t.Run(test.name, func(t *testing.T) {
			path := generatedAVSourceMediaFixture(t, ctx, ffmpeg, 2, 2, false, test.rate, test.bframes)
			source, _ := generatedClosureMediaOpenSource(t, path)
			got, err := MeasureGeneratedMP4AVSourceEndpoint(ctx, ffprobe, source, 0, 1)
			if err == nil || got != (GeneratedAVSourceCertificate{}) {
				t.Fatalf("unsupported actual source returned a candidate: %+v %v", got, err)
			}
		})
	}
}

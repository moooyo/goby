package media

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// This fixture intentionally matches the native administrator journey rather
// than replacing its AAC encoder delay with the earlier PCM fixture profile.
func TestSubtitleRemovalActualMatroskaAACCodecDelay24FPS(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set GOBY_FFMPEG and GOBY_FFPROBE for the native AAC codec-delay regression")
	}
	directory := t.TempDir()
	record := retainMediaEditTestEvidence(t, directory, "mkv")
	remove, keep, chapters := filepath.Join(directory, "remove.srt"), filepath.Join(directory, "keep.srt"), filepath.Join(directory, "chapters.ffmetadata")
	for path, contents := range map[string]string{
		remove:   "1\n00:00:01,000 --> 00:00:03,000\nRemove this English track\n",
		keep:     "1\n00:00:02,000 --> 00:00:04,000\nConserver cette piste\n",
		chapters: ";FFMETADATA1\ntitle=Selected Phase Two Source\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=3000\ntitle=Opening\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=3000\nEND=12000\ntitle=Main\n",
	} {
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(directory, "source.mkv")
	args := []string{"-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=navy:size=720x576:rate=24:duration=12",
		"-f", "lavfi", "-i", "sine=frequency=631:sample_rate=48000:duration=12",
		"-i", remove, "-i", keep, "-f", "ffmetadata", "-i", chapters,
		"-map", "0:v:0", "-map", "1:a:0", "-map", "2:s:0", "-map", "3:s:0", "-map_metadata", "4", "-map_chapters", "4", "-c:s", "srt",
		"-metadata:s:s:0", "language=eng", "-metadata:s:s:0", "title=Remove English", "-disposition:s:0", "default",
		"-metadata:s:s:1", "language=fra", "-metadata:s:s:1", "title=Keep French", "-disposition:s:1", "0",
		"-af", "asetpts=PTS+1024/SR/TB", "-c:v", "libx264", "-preset", "ultrafast", "-threads:v", "1", "-bf", "0", "-g", "24",
		"-pix_fmt", "yuv420p", "-c:a", "aac", "-threads:a", "1", "-b:a", "64000", "-t", "12", "-avoid_negative_ts", "disabled", "-f", "matroska", path}
	if _, err := runLimited(context.Background(), 30*time.Second, 4096, ffmpeg, args...); err != nil {
		record.Failure = err.Error()
		t.Fatalf("generate the original native AAC source: %v", err)
	}
	input, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err := input.Seek(13, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	stat, err := input.Stat()
	if err != nil {
		t.Fatal(err)
	}
	record.Stage = "source_delay_precondition"
	sourceContainer, err := mediaEditReadContainerProof(context.Background(), input, stat.Size(), "mkv")
	record.SourceContainer = &sourceContainer
	if err != nil {
		record.Failure = err.Error()
		t.Fatal(err)
	}
	sourceDocument, err := probeMediaEditDocument(context.Background(), ffprobe, input)
	record.SourceDocument = &sourceDocument
	if err != nil {
		record.Failure = err.Error()
		t.Fatal(err)
	}
	if len(sourceContainer.MatroskaTracks) != 4 || len(sourceDocument.Streams) != 4 {
		t.Fatal("native fixture did not retain its four source tracks")
	}
	audio := sourceContainer.MatroskaTracks[1]
	padding, paddingErr := mediaEditInteger(sourceDocument.Streams[1]["initial_padding"])
	if audio.TrackType != 2 || audio.CodecID != "A_AAC" || audio.CodecDelayNS != 21_333_333 || audio.SeekPreRollNS != 0 || audio.SampleRate != 48_000 || audio.Channels != 1 || audio.CodecPrivateBytes != 5 || paddingErr != nil || padding != 1024 {
		t.Fatalf("native AAC priming/delay precondition was weakened: raw=%+v padding=%d error=%v", audio, padding, paddingErr)
	}
	if _, err := bindMediaEditMatroskaTracks(sourceContainer, sourceDocument); err != nil {
		t.Fatalf("source codec delay was not bound to actual probe facts: %v", err)
	}
	candidate, err := os.CreateTemp(directory, "candidate-*.")
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close()
	record.Stage = "candidate_edit_and_proof"
	evidence, err := RemuxSubtitleRemoval(context.Background(), input, candidate, SubtitleRemovalOptions{FFmpegPath: ffmpeg, FFprobePath: ffprobe, Container: "mkv", StreamIndex: 2, MaxOutputBytes: 512 << 20, Timeout: 90 * time.Second})
	record.Result = &evidence
	if err != nil {
		record.Failure = err.Error()
		t.Fatalf("preserve native AAC delay while removing the selected subtitle: %v", err)
	}
	if evidence.Engine != "ffmpeg_remux" || evidence.RemovedIndex != 2 || len(evidence.RetainedStreams) != 3 || len(evidence.SourceSHA256) != 64 || len(evidence.CandidateSHA256) != 64 || len(evidence.ContainerSHA256) != 64 {
		t.Fatalf("incomplete native AAC preservation evidence: %+v", evidence)
	}
	for _, stream := range evidence.RetainedStreams {
		if stream.SourceIndex == 2 || stream.Packets <= 0 || len(stream.PayloadSHA256) != 64 || len(stream.TimingSHA256) != 64 {
			t.Fatalf("retained packet proof was weakened: %+v", stream)
		}
	}
	if evidence.RetainedStreams[0].SourceIndex != 0 || evidence.RetainedStreams[0].Packets != 288 || evidence.RetainedStreams[1].SourceIndex != 1 || evidence.RetainedStreams[1].CodecType != "audio" || evidence.RetainedStreams[2].SourceIndex != 3 {
		t.Fatalf("native A/V or subtitle mapping changed: %+v", evidence.RetainedStreams)
	}
	after, err := candidate.Stat()
	if err != nil {
		t.Fatal(err)
	}
	candidateContainer, err := mediaEditReadContainerProof(context.Background(), candidate, after.Size(), "mkv")
	record.CandidateContainer = &candidateContainer
	if err != nil {
		record.Failure = err.Error()
		t.Fatal(err)
	}
	if len(candidateContainer.MatroskaTracks) != 3 || candidateContainer.MatroskaTracks[1].CodecDelayNS != audio.CodecDelayNS || candidateContainer.MatroskaTracks[1].SampleRate != audio.SampleRate || candidateContainer.MatroskaTracks[1].CodecPrivateSHA256 != audio.CodecPrivateSHA256 {
		t.Fatal("candidate lost the exact AAC delay/source-header relationship")
	}
	record.Stage = "candidate_probe"
	info, err := (Prober{FFprobePath: ffprobe, FFmpegPath: ffmpeg, Timeout: 30 * time.Second}).ProbeFile(context.Background(), candidate)
	record.CandidateInfo = &info
	if err != nil || len(info.Streams) != 3 || len(info.Chapters) != 2 {
		record.Failure = "candidate inventory changed"
		t.Fatalf("native candidate inventory: %+v %v", info, err)
	}
	if info.Streams[0].Codec != "h264" || info.Streams[1].Codec != "aac" || info.Streams[2].CodecType != "subtitle" || info.Streams[2].Language != "fra" || info.Streams[2].IsDefault {
		t.Fatalf("native retained codec/subtitle metadata changed: %+v", info.Streams)
	}
	if info.Chapters[0].StartTicks != 0 || info.Chapters[0].EndTicks != 3*TicksPerSecond || info.Chapters[0].Title != "Opening" || info.Chapters[1].StartTicks != 3*TicksPerSecond || info.Chapters[1].EndTicks != 12*TicksPerSecond || info.Chapters[1].Title != "Main" {
		t.Fatalf("native chapters changed: %+v", info.Chapters)
	}
	record.Stage = "candidate_decode"
	decoded, err := decodeMediaEditTestCandidate(context.Background(), ffmpeg, candidate, info)
	record.CandidateDecode = &decoded
	if err != nil {
		record.Failure = err.Error()
		t.Fatal(err)
	}
	if !decoded.Complete || !decoded.ProgressEnd || decoded.VideoStreams != 1 || decoded.AudioStreams != 1 || len(decoded.MappedIndexes) != 2 || decoded.VideoFrames != 288 || decoded.OutTimeUS < 11_900_000 || decoded.OutTimeUS > 12_100_000 {
		t.Fatalf("native candidate did not completely decode retained A/V: %+v", decoded)
	}
	if offset, err := input.Seek(0, io.SeekCurrent); err != nil || offset != 13 {
		t.Fatalf("borrowed source descriptor moved: %d %v", offset, err)
	}
	record.Stage = "complete"
}

package media

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"
)

func TestDolbyVisionIngestFastProbeKeepsDeclarationsAndDefersBothPasses(t *testing.T) {
	const configuration = `{"side_data_type":"DOVI configuration record","dv_profile":8,"dv_level":6,"rpu_present_flag":1,"el_present_flag":0,"bl_present_flag":1,"dv_bl_signal_compatibility_id":1,"dv_md_compression":"none"}`
	const video = `"codec_type":"video","codec_name":"hevc","pix_fmt":"yuv420p10le","color_space":"bt2020nc","color_transfer":"smpte2084","color_primaries":"bt2020"`
	document := `{"format":{"format_name":"mov,mp4","duration":"10"},"streams":[` +
		`{"index":7,` + video + `,"side_data_list":[` + configuration + `]},` +
		`{"index":9,` + video + `,"side_data_list":[` + configuration + `]},` +
		`{"index":15,` + video + `,"side_data_list":[{"side_data_type":"DOVI configuration record","dv_profile":8}]}]}`
	probe := analysisProcessTestTool(t, `printf 'probe\n' >> "$0.calls"; cat "$0.json"`)
	if err := os.WriteFile(probe+".json", []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	tool := analysisProcessTestTool(t, `
case " $* " in
  *" -map 0:7 "*) stream=7 ;;
  *" -map 0:9 "*) stream=9 ;;
  *) exit 60 ;;
esac
case " $* " in
  *" -i /proc/self/fd/3 "*) ;;
  *) exit 61 ;;
esac
case "$*" in
  *dovi_rpu=compression=none*)
    printf 'validate %s\n' "$stream" >> "$0.calls"
    test -s /proc/self/fd/3
    printf '[hevc_mp4toannexb @ 0x123] No parameter sets in the extradata\n' >&2 ;;
  *)
    printf 'extract %s\n' "$stream" >> "$0.calls"
    cat /proc/self/fd/3 ;;
esac`)
	fixture := append(dolbyVisionAccessUnitFixture(true), dolbyVisionAccessUnitFixture(true)...)
	file, err := os.Create(filepath.Join(t.TempDir(), "source.hevc"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write(fixture); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(3, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fullProber := Prober{FFprobePath: probe, FFmpegPath: tool}
	fastProber := fullProber
	fastProber.SkipDolbyVisionRPU = true
	fast, err := fastProber.ProbeFile(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tool + ".calls"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fast probe launched FFmpeg: %v", err)
	}
	if fast.ProbeVersion != CurrentProbeVersion || fastProber.CacheVersion() != fullProber.CacheVersion() ||
		fast.Size != int64(len(fixture)) || fast.FileChangeTimeNs == 0 || !slices.Equal(PendingDolbyVisionStreams(fast), []int{7, 9}) {
		t.Fatalf("fast probe discarded required technical facts: %+v", fast)
	}
	for i, stream := range fast.Streams {
		if stream.VideoRange != "DOVI" || !stream.VideoRangeKnown || (stream.DolbyVision == nil) != (i == 2) ||
			DolbyVisionConversionSupported(stream) || stream.DolbyVision != nil && stream.DolbyVision.RPUVerified {
			t.Fatalf("fast probe converted a Dolby Vision declaration into proof or HDR10: %+v", stream)
		}
	}
	phases := 0
	work := WithSourceReadPhase(ctx, func(work context.Context, read func(context.Context) error) error {
		phases++
		return read(work)
	})
	evidence, err := VerifyDolbyVisionIngest(work, tool, file, fast, 7)
	if err != nil || !evidence.Valid() || !evidence.Verified || evidence.StreamIndex != 7 || evidence.Profile != 8 ||
		!evidence.ResidualDisabled || evidence.FrameCount != 2 || !evidence.InBandParameterSets || phases != 1 {
		t.Fatalf("incorrect verification of the selected stream: %+v, phases=%d, %v", evidence, phases, err)
	}
	assertDolbyVisionIngestContractCalls(t, probe, "probe\n")
	assertDolbyVisionIngestContractCalls(t, tool, "extract 7\nvalidate 7\n")
	full, err := fullProber.ProbeFile(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	projected := ApplyDolbyVisionRPUEvidence(fast, []DolbyVisionRPUEvidence{evidence})
	if !reflect.DeepEqual(projected.Streams[0], full.Streams[0]) || !full.Streams[1].DolbyVision.RPUVerified ||
		projected.Streams[1].DolbyVision.RPUVerified || !reflect.DeepEqual(projected.Streams[2], full.Streams[2]) ||
		len(PendingDolbyVisionStreams(full)) != 0 {
		t.Fatalf("deferred and synchronous evidence differ or were applied to another stream: %+v, %+v", projected.Streams, full.Streams)
	}
	assertDolbyVisionIngestContractCalls(t, probe, "probe\nprobe\n")
	assertDolbyVisionIngestContractCalls(t, tool, "extract 7\nvalidate 7\nextract 7\nvalidate 7\nextract 9\nvalidate 9\n")
	if offset, err := file.Seek(0, io.SeekCurrent); err != nil || offset != 3 {
		t.Fatalf("borrowed source descriptor was closed or seeked: %d, %v", offset, err)
	}
}

func TestDolbyVisionIngestDistinguishesHardSourceFailureFromUnprovenVideo(t *testing.T) {
	const index = 7
	for _, test := range []struct {
		name   string
		body   string
		reason string
		block  error
		stale  bool
	}{
		{name: "retirement unknown", block: ErrProcessRetirementUnknown},
		{name: "rejected source admission", block: errors.New("test source admission refused")},
		{name: "stale accepted source", stale: true},
		{name: "source changed during read", body: `printf 'change' >> "$0.source"; exit 1`},
		{name: "invalid source bytes", body: "printf 'invalid access unit'", reason: dolbyVisionRPUInvalidScan},
		{name: "native validation failure", body: `case "$*" in *dovi_rpu=compression=none*) exit 1 ;; *) cat /proc/self/fd/3 ;; esac`, reason: dolbyVisionRPUScanFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			tool := analysisProcessTestTool(t, `printf 'spawn\n' >> "$0.calls"`+"\n"+test.body)
			if err := os.WriteFile(tool+".source", dolbyVisionAccessUnitFixture(true), 0600); err != nil {
				t.Fatal(err)
			}
			file, err := os.Open(tool + ".source")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			stat, err := file.Stat()
			if err != nil {
				t.Fatal(err)
			}
			info := Info{ProbeVersion: CurrentProbeVersion, Size: stat.Size(), FileChangeTimeNs: FileChangeTime(stat),
				Streams: []Stream{ingestContractDolbyVisionStream(index, 8, "none")}}
			if test.stale {
				info.FileChangeTimeNs++
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			work := context.Context(ctx)
			if test.block != nil {
				work = WithSourceReadPhase(ctx, func(context.Context, func(context.Context) error) error { return test.block })
			}
			got, err := VerifyDolbyVisionIngest(work, tool, file, info, index)
			if test.reason == "" {
				if err == nil || (test.block != nil && !errors.Is(err, test.block)) || got != (DolbyVisionRPUEvidence{}) {
					t.Fatalf("hard source failure became an evidence record: %+v, %v", got, err)
				}
			} else if err != nil || got != (DolbyVisionRPUEvidence{StreamIndex: index, Reason: test.reason}) || !got.Valid() ||
				got.Retryable() != (test.reason == dolbyVisionRPUScanFailed) {
				t.Fatalf("unproven scan retained physical proof or lost its closed result: %+v, %v", got, err)
			}
			if test.stale || test.block != nil {
				if _, err := os.Stat(tool + ".calls"); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("ineligible or blocked verification started a process: %v", err)
				}
			}
			stream := ApplyDolbyVisionRPUEvidence(info, []DolbyVisionRPUEvidence{got}).Streams[0]
			if stream.VideoRange != "DOVI" || !stream.VideoRangeKnown || stream.DolbyVision.RPUVerified || DolbyVisionConversionSupported(stream) {
				t.Fatalf("unproven scan changed Dolby Vision facts or enabled conversion: %+v", stream)
			}
		})
	}
}

func assertDolbyVisionIngestContractCalls(t *testing.T, tool, want string) {
	t.Helper()
	got, err := os.ReadFile(tool + ".calls")
	if err != nil || string(got) != want {
		t.Fatalf("tool invocations = %q, want %q: %v", got, want, err)
	}
}

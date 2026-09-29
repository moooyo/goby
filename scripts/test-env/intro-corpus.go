//go:build ignore

// intro-corpus extracts production features and evaluates frozen source cohorts.
// It never downloads media, assigns labels, or publishes application markers.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/media"
)

type sourceCase struct {
	ID      string `json:"id"`
	Series  string `json:"series"`
	Role    string `json:"role"`
	Episode string `json:"episode"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
}

type sourceFeatures struct {
	Case     sourceCase
	Info     media.Info
	Features media.IntroFeatures
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func writeJSON(path string, value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	check(err)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	check(err)
	_, err = file.Write(append(data, '\n'))
	check(err)
	check(file.Close())
}

func selectedStream(info media.Info, kind string) int {
	selected := -1
	defaultStream := false
	for _, stream := range info.Streams {
		if stream.CodecType != kind || stream.IsExternal || stream.IsAttachedPicture {
			continue
		}
		if selected == -1 || stream.IsDefault && !defaultStream || stream.IsDefault == defaultStream && stream.Index < selected {
			selected, defaultStream = stream.Index, stream.IsDefault
		}
	}
	if selected < 0 {
		panic("required stream is absent")
	}
	return selected
}

func main() {
	mode := flag.String("mode", "", "extract or analyze")
	sources := flag.String("sources", "", "frozen public-source JSON inventory")
	labels := flag.String("labels", "", "pre-detection source labels")
	labelHash := flag.String("labels-sha256", "", "frozen label digest")
	series := flag.String("series", "", "one exact selected source series")
	features := flag.String("features", "", "feature input/output directory")
	output := flag.String("output", "", "new result JSON for analyze")
	optionsFile := flag.String("options", "", "optional complete calibration-only detector options JSON")
	ffmpeg := flag.String("ffmpeg", "/opt/ffmpeg/9.0.1/bin/ffmpeg", "production FFmpeg")
	ffprobe := flag.String("ffprobe", "/opt/ffmpeg/9.0.1/bin/ffprobe", "production FFprobe")
	helper := flag.String("fingerprint", "/opt/goby-intro-fingerprint/bin/goby-intro-fingerprint", "production fingerprint helper")
	flag.Parse()
	if (*mode != "extract" && *mode != "analyze") || *series == "" || !filepath.IsAbs(*features) {
		panic("explicit mode, series and absolute feature directory required")
	}
	labelBytes, err := os.ReadFile(*labels)
	check(err)
	digest := sha256.Sum256(labelBytes)
	if len(*labelHash) != 64 || hex.EncodeToString(digest[:]) != *labelHash {
		panic("frozen label digest mismatch")
	}
	var frozen map[string]any
	check(json.Unmarshal(labelBytes, &frozen))
	if frozen["detectorOutputsUsed"] != false {
		panic("independent pre-detection source labels required")
	}
	raw, err := os.ReadFile(*sources)
	check(err)
	var all []sourceCase
	check(json.Unmarshal(raw, &all))
	var cases []sourceCase
	for _, item := range all {
		if item.Series == *series {
			cases = append(cases, item)
		}
	}
	if len(cases) < 3 || len(cases) > 32 {
		panic("complete bounded cohort requires at least three independent episodes")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	cohort := introdetect.Cohort{Key: *series}
	for _, item := range cases {
		path := filepath.Join(*features, item.ID+".json")
		var stored sourceFeatures
		if *mode == "extract" {
			file, err := os.Open(item.Path)
			check(err)
			hash := sha256.New()
			_, err = io.Copy(hash, file)
			check(err)
			if hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
				panic("original content identity changed")
			}
			bounded, stop := context.WithTimeout(ctx, 15*time.Minute)
			info, err := (media.Prober{FFprobePath: *ffprobe, FFmpegPath: *ffmpeg}).ProbeFile(bounded, file)
			check(err)
			data, err := (media.AnalysisExtractor{FFmpegPath: *ffmpeg, FFprobePath: *ffprobe, FingerprintPath: *helper}).ExtractIntro(bounded, file, info,
				media.IntroAnalysisRequest{AudioStreamIndex: selectedStream(info, "audio"), VideoStreamIndex: selectedStream(info, "video"), VisualIntervalTicks: media.TicksPerSecond / 2})
			check(err)
			stop()
			check(file.Close())
			stored = sourceFeatures{item, info, data}
			writeJSON(path, stored)
			fmt.Printf("extracted case=%s audio=%d visual=%d\n", item.ID, len(data.Audio), len(data.Visual))
		} else {
			data, err := os.ReadFile(path)
			check(err)
			check(json.Unmarshal(data, &stored))
			if stored.Case != item {
				panic("feature source inventory changed")
			}
		}
		cohort.Episodes = append(cohort.Episodes, introdetect.Episode{EpisodeKey: item.Series + ":" + item.Episode,
			SourceKey: item.SHA256, ContentIdentity: item.SHA256, AlgorithmProfile: stored.Features.AlgorithmProfile,
			DurationTicks: stored.Info.DurationTicks, AudioBoundaryUncertaintyTicks: stored.Features.AudioBoundaryUncertaintyTicks,
			Audio: stored.Features.Audio, Visual: stored.Features.Visual})
	}
	if *mode == "analyze" {
		bounded, stop := context.WithTimeout(ctx, 5*time.Minute)
		defer stop()
		options := introdetect.DefaultOptions()
		if *optionsFile != "" {
			data, err := os.ReadFile(*optionsFile)
			check(err)
			check(json.Unmarshal(data, &options))
			for _, item := range cases {
				if item.Role != "calibration" {
					panic("option experiments require calibration-only sources")
				}
			}
		}
		result, err := introdetect.Analyze(bounded, cohort, options)
		check(err)
		writeJSON(*output, struct {
			LabelsSHA256 string
			Result       introdetect.Result
		}{*labelHash, result})
		fmt.Printf("analyzed series=%s episodes=%d groups=%d comparisons=%d version=%s\n", *series, len(result.Episodes), len(result.Groups), result.Comparisons, result.Version)
	}
}

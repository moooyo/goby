package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

const backgroundClipProbeBytes = 512 << 10

type backgroundClipProbeDocument struct {
	Packets []struct {
		StreamIndex *int   `json:"stream_index"`
		PTS         *int64 `json:"pts"`
		DTS         *int64 `json:"dts"`
		Duration    *int64 `json:"duration"`
		Flags       string `json:"flags"`
	} `json:"packets"`
	Streams []struct {
		Index          *int   `json:"index"`
		Codec          string `json:"codec_name"`
		Type           string `json:"codec_type"`
		PixelFormat    string `json:"pix_fmt"`
		Width          int    `json:"width"`
		Height         int    `json:"height"`
		TimeBase       string `json:"time_base"`
		FrameRate      string `json:"avg_frame_rate"`
		RealFrameRate  string `json:"r_frame_rate"`
		ColorRange     string `json:"color_range"`
		ColorSpace     string `json:"color_space"`
		ColorTransfer  string `json:"color_transfer"`
		ColorPrimaries string `json:"color_primaries"`
		SideData       []struct {
			Type string `json:"side_data_type"`
		} `json:"side_data_list"`
	} `json:"streams"`
	Format struct {
		Name string `json:"format_name"`
	} `json:"format"`
}

func validateBackgroundClip(ctx context.Context, ffprobe, ffmpeg *analysisTool, encoded []byte, plan backgroundClipPlan, limits AnalysisLimits) error {
	probeArgs := []string{"-v", "error", "-max_alloc", "268435456", "-threads", "1", "-probesize", "8388608", "-analyzeduration", "10000000",
		"-protocol_whitelist", "pipe", "-format_whitelist", "mov,mp4,m4a,3gp,3g2,mj2", "-f", "mp4",
		"-show_entries", "packet=stream_index,pts,dts,duration,flags:stream=index,codec_name,codec_type,pix_fmt,width,height,time_base,avg_frame_rate,r_frame_rate,color_range,color_space,color_transfer,color_primaries:stream_side_data=side_data_type:format=format_name",
		"-of", "json", "-i", "pipe:0"}
	sink := &analysisDiscardStderr{}
	err := runAnalysisProcess(ctx, "/proc/self/fd/3", nil, bytes.NewReader(encoded), probeArgs, limits.Timeout, backgroundClipProbeBytes, sink,
		func(reader io.Reader) error { return parseBackgroundClipProbe(reader, plan) }, ffprobe.file)
	if err == nil {
		err = sink.failure()
	}
	if err != nil {
		return err
	}
	// Decode every encoded picture, with no sampling or frame synthesis, into a
	// tiny raster. This catches broken payloads that a container probe accepts.
	decodeArgs := []string{"-hide_banner", "-nostdin", "-nostats", "-loglevel", "error", "-xerror", "-max_alloc", "268435456",
		"-threads", "2", "-max_pixels", "2073600", "-filter_threads", "1", "-filter_complex_threads", "1",
		"-protocol_whitelist", "pipe", "-format_whitelist", "mov,mp4,m4a,3gp,3g2,mj2", "-f", "mp4", "-i", "pipe:0",
		"-map", "0:0", "-an", "-sn", "-dn", "-vf", "scale=32:18:flags=area,format=gray", "-fps_mode:v", "passthrough",
		"-c:v", "rawvideo", "-threads:v", "1", "-pix_fmt", "gray", "-f", "rawvideo", "pipe:1"}
	sink = &analysisDiscardStderr{}
	err = runAnalysisProcess(ctx, "/proc/self/fd/3", nil, bytes.NewReader(encoded), decodeArgs, limits.Timeout, int64(plan.frames)*32*18, sink,
		func(reader io.Reader) error { return inspectBackgroundClipPictures(ctx, reader, plan.frames) }, ffmpeg.file)
	if err == nil {
		err = sink.failure()
	}
	return err
}

func parseBackgroundClipProbe(reader io.Reader, plan backgroundClipPlan) error {
	bounded := &io.LimitedReader{R: reader, N: backgroundClipProbeBytes + 1}
	data, err := io.ReadAll(bounded)
	if bounded.N <= 0 {
		return ErrAnalysisBudget
	}
	if err != nil {
		return err
	}
	if _, err := mediaEditDecodeJSON(data); err != nil {
		return fmt.Errorf("%w: malformed or duplicate background clip metadata", ErrAnalysisUnproven)
	}
	var document backgroundClipProbeDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("%w: malformed background clip probe", ErrAnalysisUnproven)
	}
	if document.Format.Name != "mov,mp4,m4a,3gp,3g2,mj2" || len(document.Streams) != 1 || len(document.Packets) != plan.frames {
		return fmt.Errorf("%w: background clip stream or packet count", ErrAnalysisUnproven)
	}
	stream := document.Streams[0]
	if stream.Index == nil || *stream.Index != 0 || stream.Codec != "h264" || stream.Type != "video" || stream.PixelFormat != "yuv420p" ||
		stream.Width != plan.width || stream.Height != plan.height || stream.TimeBase != "1/"+strconv.Itoa(backgroundClipTimescale) ||
		stream.FrameRate != "24/1" || stream.RealFrameRate != "24/1" || !backgroundClipSDRTransfer(stream.ColorTransfer) {
		return fmt.Errorf("%w: background clip output profile", ErrAnalysisUnproven)
	}
	if plan.toneMapped && (stream.ColorRange != "tv" || stream.ColorSpace != "bt709" || stream.ColorTransfer != "bt709" || stream.ColorPrimaries != "bt709") {
		return fmt.Errorf("%w: background clip is not declared SDR after tone mapping", ErrAnalysisUnproven)
	}
	for _, sideData := range stream.SideData {
		switch sideData.Type {
		case "DOVI configuration record", "Dolby Vision RPU Data", "Dolby Vision Metadata", "Mastering display metadata", "Content light level metadata", "HDR Dynamic Metadata SMPTE2094-40 (HDR10+)":
			return fmt.Errorf("%w: background clip retains source HDR metadata", ErrAnalysisUnproven)
		}
	}
	for index, packet := range document.Packets {
		if packet.StreamIndex == nil || *packet.StreamIndex != 0 || packet.PTS == nil || packet.DTS == nil || packet.Duration == nil ||
			*packet.PTS != int64(index)*1000 || *packet.DTS != *packet.PTS || *packet.Duration != 1000 ||
			!backgroundClipPacketFlags(packet.Flags) || index == 0 && packet.Flags[0] != 'K' {
			return fmt.Errorf("%w: background clip packet timeline", ErrAnalysisUnproven)
		}
	}
	return nil
}

func backgroundClipPacketFlags(flags string) bool {
	return flags == "K_" || flags == "K__" || flags == "__" || flags == "___"
}

func inspectBackgroundClipPictures(ctx context.Context, reader io.Reader, frames int) error {
	if frames < backgroundClipFPS || frames > backgroundClipFPS*60 {
		return ErrAnalysisBudget
	}
	const pixels = 32 * 18
	var picture, previous [pixels]byte
	black, comparisons, static := 0, 0, 0
	for index := 0; index < frames; index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := io.ReadFull(reader, picture[:]); err != nil {
			return fmt.Errorf("%w: incomplete decoded background clip: %v", ErrAnalysisUnproven, err)
		}
		var sum, bright int
		for _, value := range picture {
			sum += int(value)
			if value > 16 {
				bright++
			}
		}
		if sum <= 8*pixels && bright <= pixels/20 {
			black++
		}
		// Compare pictures one second apart. Only near-identical material over
		// almost the entire clip is rejected; this is not a scene-quality score.
		if index%backgroundClipFPS == 0 {
			if index > 0 {
				difference := 0
				for pixel, value := range picture {
					delta := int(value) - int(previous[pixel])
					if delta < 0 {
						delta = -delta
					}
					difference += delta
				}
				comparisons++
				if difference <= pixels {
					static++
				}
			}
			previous = picture
		}
	}
	var trailing [1]byte
	if n, err := reader.Read(trailing[:]); n != 0 || err != io.EOF {
		return fmt.Errorf("%w: extra decoded background clip pictures", ErrAnalysisUnproven)
	}
	if black*10 >= frames*9 || comparisons >= 2 && static*20 >= comparisons*19 {
		return ErrBackgroundClipUnusable
	}
	return nil
}

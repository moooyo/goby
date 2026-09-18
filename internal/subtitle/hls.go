package subtitle

import (
	"bytes"
	"fmt"
	"strings"
)

// RenderHLS preserves source-relative cue times, applies the requested caption
// delay, and binds the result to the measured output transport clock. Imported
// timestamp maps belong to the source representation and are never reused.
// The caller supplies the post-mux minus pre-mux clock difference in ticks.
func RenderHLS(document Document, offsetTicks, clockDeltaTicks int64) (Result, error) {
	if clockDeltaTicks < -MaxOffsetTicks || clockDeltaTicks > MaxOffsetTicks {
		return Result{}, fmt.Errorf("%w: HLS clock difference exceeds 24 hours", ErrInvalidRange)
	}
	rendered, err := Render(document, Options{Format: FormatWebVTT, CopyTimestamps: true, OffsetTicks: offsetTicks})
	if err != nil {
		return Result{}, err
	}
	separator := bytes.Index(rendered.Data, []byte("\n\n"))
	if separator < 0 {
		return Result{}, fmt.Errorf("%w: missing rendered WebVTT header", ErrInvalidDocument)
	}
	lines := strings.Split(string(rendered.Data[:separator]), "\n")
	if len(lines) == 0 || !validSignature(lines[0]) {
		return Result{}, fmt.Errorf("%w: invalid rendered WebVTT signature", ErrInvalidDocument)
	}
	// A negative transport difference uses a positive millisecond LOCAL anchor.
	// Its small nonnegative MPEGTS remainder retains submillisecond precision
	// without wrapping a negative number into a distant 33-bit timestamp epoch.
	localTicks := int64(0)
	if clockDeltaTicks < 0 {
		localTicks = ((-clockDeltaTicks + TicksPerMillisecond - 1) / TicksPerMillisecond) * TicksPerMillisecond
	}
	transportTicks := clockDeltaTicks + localTicks
	transportPTS := (transportTicks*90_000 + TicksPerSecond/2) / TicksPerSecond
	milliseconds := localTicks / TicksPerMillisecond
	local := fmt.Sprintf("%02d:%02d:%02d.%03d", milliseconds/3_600_000, (milliseconds/60_000)%60, (milliseconds/1000)%60, milliseconds%1000)
	mapping := fmt.Sprintf("X-TIMESTAMP-MAP=LOCAL:%s,MPEGTS:%d", local, transportPTS)
	header := make([]string, 0, len(lines)+1)
	header = append(header, lines[0], mapping)
	for _, line := range lines[1:] {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "X-TIMESTAMP-MAP") {
			continue
		}
		header = append(header, line)
	}
	prefix := strings.Join(header, "\n") + "\n\n"
	body := rendered.Data[separator+2:]
	if len(prefix) > MaxOutputBytes-len(body) {
		return Result{}, fmt.Errorf("%w: mapped HLS subtitles exceed %d bytes", ErrLimitExceeded, MaxOutputBytes)
	}
	data := make([]byte, 0, len(prefix)+len(body))
	data = append(data, prefix...)
	data = append(data, body...)
	return Result{Data: data, ContentType: rendered.ContentType}, nil
}

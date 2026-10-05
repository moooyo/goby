package media

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var audioWaveformFrameLine = regexp.MustCompile(`\bn:([0-9]+) pts:([^ ]+) pts_time:[^ ]+ fmt:flt channels:([0-9]+) chlayout:.*? rate:([0-9]+) nb_samples:([0-9]+) checksum:([0-9A-Fa-f]{8})(?: |$)`)

type audioWaveformFrame struct {
	pts      int64
	samples  int
	checksum uint32
}

type audioWaveformLog struct {
	mu                sync.Mutex
	stream            Stream
	audit             *analysisPacketPTS
	pending           []byte
	frames            chan audioWaveformFrame
	done              chan struct{}
	count, frameLimit int
	err               error
	closed            bool
}

func newAudioWaveformLog(stream Stream, duration int64) (*audioWaveformLog, error) {
	// Tiny source frames may produce more records than the usual codec block
	// size. The finite packet and process byte budgets still bound work.
	packets := min(int((duration/TicksPerSecond+3)*int64(stream.SampleRate)), 32_000_000)
	audit, err := newAnalysisPacketPTSAudit(stream, packets)
	if err != nil {
		return nil, err
	}
	return &audioWaveformLog{stream: stream, audit: audit, frames: make(chan audioWaveformFrame, 4096), done: make(chan struct{}), frameLimit: packets}, nil
}

func (log *audioWaveformLog) Write(data []byte) (int, error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.err != nil {
		return 0, log.err
	}
	if log.closed {
		return 0, io.ErrClosedPipe
	}
	consumed := 0
	for len(data) > 0 {
		end := bytes.IndexByte(data, '\n')
		complete := end >= 0
		if !complete {
			end = len(data)
		}
		if len(log.pending)+end > 16<<10 {
			log.err = ErrAnalysisBudget
			return consumed, log.err
		}
		log.pending = append(log.pending, data[:end]...)
		consumed += end
		data = data[end:]
		if !complete {
			break
		}
		consumed++
		data = data[1:]
		if err := log.line(string(log.pending)); err != nil {
			log.err = err
			return consumed, err
		}
		log.pending = log.pending[:0]
	}
	return consumed, nil
}

func (log *audioWaveformLog) line(line string) error {
	if strings.Contains(line, "[error]") || strings.Contains(line, "[fatal]") || strings.Contains(line, "[panic]") {
		return fmt.Errorf("%w: waveform decoder error", ErrAnalysisUnproven)
	}
	if _, err := log.audit.line(line); err != nil {
		return fmt.Errorf("%w: waveform source PTS: %v", ErrAnalysisUnproven, err)
	}
	matches := audioWaveformFrameLine.FindAllStringSubmatch(line, -1)
	if len(matches) == 0 {
		if analysisAudioFrameStart.MatchString(line) {
			return fmt.Errorf("%w: incomplete waveform frame record", ErrAnalysisUnproven)
		}
		return nil
	}
	if len(matches) != 1 {
		return fmt.Errorf("%w: ambiguous waveform frame", ErrAnalysisUnproven)
	}
	fields := matches[0]
	number, nerr := strconv.Atoi(fields[1])
	pts, perr := analysisPTSInteger(fields[2])
	channels, cerr := strconv.Atoi(fields[3])
	rate, rerr := strconv.Atoi(fields[4])
	samples, serr := strconv.Atoi(fields[5])
	checksum, herr := strconv.ParseUint(fields[6], 16, 32)
	if nerr != nil || perr != nil || cerr != nil || rerr != nil || serr != nil || herr != nil || number != log.count || channels != log.stream.Channels || rate != log.stream.SampleRate || samples < 1 || samples > audioWaveformFrameSamples {
		return fmt.Errorf("%w: waveform PCM format or sequence changed", ErrAnalysisUnproven)
	}
	if log.count >= log.frameLimit {
		return ErrAnalysisBudget
	}
	log.count++
	select {
	case log.frames <- audioWaveformFrame{pts: pts, samples: samples, checksum: uint32(checksum)}:
		return nil
	default:
		return fmt.Errorf("%w: waveform metadata queue", ErrAnalysisBudget)
	}
}

func (log *audioWaveformLog) Close(err error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.closed {
		return
	}
	log.closed = true
	if log.err == nil {
		log.err = err
	}
	if log.err == nil && (len(log.pending) != 0 || log.count == 0 || log.audit.packets == 0) {
		log.err = fmt.Errorf("%w: incomplete waveform diagnostics", ErrAnalysisUnproven)
	}
	close(log.done)
}

func (log *audioWaveformLog) next(ctx context.Context) (audioWaveformFrame, error) {
	select {
	case frame := <-log.frames:
		return frame, nil
	case <-ctx.Done():
		return audioWaveformFrame{}, ctx.Err()
	case <-log.done:
		select {
		case frame := <-log.frames:
			return frame, nil
		default:
		}
		log.mu.Lock()
		defer log.mu.Unlock()
		if log.err != nil {
			return audioWaveformFrame{}, log.err
		}
		return audioWaveformFrame{}, io.EOF
	}
}

func (log *audioWaveformLog) result() error {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.err != nil {
		return log.err
	}
	if !log.closed || len(log.frames) != 0 {
		return fmt.Errorf("%w: unmatched waveform diagnostics", ErrAnalysisUnproven)
	}
	return nil
}

package subtitle

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// LiveParserOptions limits each retained parser unit, not the lifetime of an
// endless pipe. Source readers and callback cancellation belong to the caller.
type LiveParserOptions struct {
	MaxHeaderBytes   int
	MaxBlockBytes    int
	MaxMetadataBytes int
	MaxLineBytes     int
}

type LiveHeader struct {
	Lines        []string
	TimestampMap *LiveTimestampMap
}

// LiveBatch owns its slices and contains at most one complete cue. HeaderOnly
// callbacks also communicate sparse streams and STYLE/REGION/NOTE blocks without
// manufacturing a cue or declaring any source-clock watermark.
type LiveBatch struct {
	Document   Document
	Header     LiveHeader
	HeaderOnly bool
	// Note contains only the current NOTE block. It is never retained in the
	// parser's metadata history or interpreted as a clock by this package.
	Note string
}

// ReadLiveWebVTT calls emit after each complete blank-terminated header or block,
// without waiting for EOF. A final complete block at EOF is also accepted.
// UTF-8, CRLF, LF and CR are supported; UTF-16 must be converted by the caller.
// Callback errors stop reading immediately. Close the source to cancel a blocked
// read. Concatenated WEBVTT headers start a fresh representation, not a generation.
func ReadLiveWebVTT(reader io.Reader, options LiveParserOptions, emit func(LiveBatch) error) error {
	if reader == nil || emit == nil {
		return ErrInvalidDocument
	}
	options, err := normalizeLiveParserOptions(options)
	if err != nil {
		return err
	}
	lines := liveLineReader{reader: bufio.NewReaderSize(reader, 4096), limit: options.MaxLineBytes}
	parser := liveWebVTTParser{options: options, emit: emit}
	var block []string
	blockBytes := 0
	firstLine := true
	for {
		line, present, readErr := lines.next()
		if present {
			if firstLine {
				line = strings.TrimPrefix(line, "\ufeff")
				firstLine = false
			}
			if !utf8.ValidString(line) || strings.ContainsRune(line, 0) {
				return ErrInvalidEncoding
			}
			if blank(line) {
				if len(block) != 0 {
					if err := parser.block(block); err != nil {
						return err
					}
					block, blockBytes = nil, 0
				}
			} else {
				limit := options.MaxBlockBytes
				possibleHeader := len(block) == 0 && validSignature(line) || len(block) > 0 && validSignature(block[0])
				if len(block) == 1 && strings.Contains(line, "-->") || len(block) > 1 && strings.Contains(block[1], "-->") {
					possibleHeader = false
				}
				if !parser.haveHeader || possibleHeader {
					limit = options.MaxHeaderBytes
				}
				if len(line)+1 > limit-blockBytes {
					return ErrLimitExceeded
				}
				blockBytes += len(line) + 1
				block = append(block, line)
			}
		}
		if readErr == nil {
			continue
		}
		if !errors.Is(readErr, io.EOF) {
			return readErr
		}
		if len(block) != 0 {
			if err := parser.block(block); err != nil {
				return err
			}
		}
		if !parser.haveHeader {
			return fmt.Errorf("%w: missing live WEBVTT header", ErrInvalidDocument)
		}
		return nil
	}
}

type liveLineReader struct {
	reader *bufio.Reader
	limit  int
	skipLF bool
}

func (r *liveLineReader) next() (string, bool, error) {
	var line []byte
	for {
		value, err := r.reader.ReadByte()
		if err != nil {
			return string(line), len(line) != 0, err
		}
		if r.skipLF {
			r.skipLF = false
			if value == '\n' {
				continue
			}
		}
		if value == '\n' {
			return string(line), true, nil
		}
		if value == '\r' {
			r.skipLF = true
			return string(line), true, nil
		}
		if len(line) >= r.limit {
			return "", false, ErrLimitExceeded
		}
		line = append(line, value)
	}
}

type liveWebVTTParser struct {
	options       LiveParserOptions
	emit          func(LiveBatch) error
	haveHeader    bool
	hadCue        bool
	header        LiveHeader
	metadata      []metadataBlock
	metadataBytes int
}

func (p *liveWebVTTParser) block(lines []string) error {
	if !p.haveHeader || validSignature(lines[0]) && (len(lines) == 1 || !strings.Contains(lines[1], "-->")) {
		header, err := parseLiveHeader(lines)
		if err != nil {
			return err
		}
		p.header, p.haveHeader, p.hadCue = header, true, false
		p.metadata, p.metadataBytes = nil, 0
		return p.publish(Document{Format: FormatWebVTT}, true, "")
	}
	if isMetadata(lines[0]) {
		kind := strings.TrimRight(lines[0], " \t")
		block := metadataBlock{text: strings.Join(lines, "\n")}
		if kind == "STYLE" || kind == "REGION" {
			if p.hadCue || strings.Contains(block.text, "-->") {
				return ErrInvalidDocument
			}
			if len(block.text)+2 > p.options.MaxMetadataBytes-p.metadataBytes {
				return ErrLimitExceeded
			}
			p.metadataBytes += len(block.text) + 2
			p.metadata = append(p.metadata, block)
			return p.publish(Document{Format: FormatWebVTT}, true, "")
		}
		return p.publish(Document{Format: FormatWebVTT, blocks: []metadataBlock{block}}, true, block.text)
	}
	cue, err := parseCue(lines, FormatWebVTT)
	if err != nil {
		return err
	}
	if err := validateCues([]Cue{cue}); err != nil {
		return err
	}
	p.hadCue = true
	return p.publish(Document{Format: FormatWebVTT, Cues: []Cue{cue}}, false, "")
}

func (p *liveWebVTTParser) publish(document Document, headerOnly bool, note string) error {
	document.header = liveCloneStrings(p.header.Lines)
	document.blocks = append(append([]metadataBlock(nil), p.metadata...), document.blocks...)
	header := LiveHeader{Lines: liveCloneStrings(p.header.Lines)}
	if p.header.TimestampMap != nil {
		value := *p.header.TimestampMap
		header.TimestampMap = &value
	}
	return p.emit(LiveBatch{Document: document, Header: header, HeaderOnly: headerOnly, Note: note})
}

func normalizeLiveParserOptions(options LiveParserOptions) (LiveParserOptions, error) {
	if options.MaxHeaderBytes == 0 {
		options.MaxHeaderBytes = 16 << 10
	}
	if options.MaxBlockBytes == 0 {
		options.MaxBlockBytes = 512 << 10
	}
	if options.MaxMetadataBytes == 0 {
		options.MaxMetadataBytes = 64 << 10
	}
	if options.MaxLineBytes == 0 {
		options.MaxLineBytes = MaxLineBytes
	}
	if options.MaxHeaderBytes < 1 || options.MaxHeaderBytes > MaxInputBytes ||
		options.MaxBlockBytes < 1 || options.MaxBlockBytes > MaxInputBytes ||
		options.MaxMetadataBytes < 1 || options.MaxMetadataBytes > MaxInputBytes ||
		options.MaxLineBytes < 1 || options.MaxLineBytes > MaxLineBytes {
		return LiveParserOptions{}, ErrLimitExceeded
	}
	return options, nil
}

func parseLiveHeader(lines []string) (LiveHeader, error) {
	if len(lines) == 0 || !validSignature(lines[0]) {
		return LiveHeader{}, ErrInvalidDocument
	}
	header := LiveHeader{Lines: liveCloneStrings(lines)}
	for _, line := range lines[1:] {
		if strings.Contains(line, "-->") {
			return LiveHeader{}, ErrInvalidDocument
		}
		value := strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToUpper(value), "X-TIMESTAMP-MAP") {
			continue
		}
		if header.TimestampMap != nil {
			return LiveHeader{}, ErrLiveTimestampMap
		}
		name, fields, ok := strings.Cut(value, "=")
		if !ok || !strings.EqualFold(name, "X-TIMESTAMP-MAP") {
			return LiveHeader{}, ErrLiveTimestampMap
		}
		mapping := LiveTimestampMap{}
		local, transport := false, false
		parts := strings.Split(fields, ",")
		if len(parts) != 2 {
			return LiveHeader{}, ErrLiveTimestampMap
		}
		for _, part := range parts {
			key, raw, ok := strings.Cut(strings.TrimSpace(part), ":")
			if !ok {
				return LiveHeader{}, ErrLiveTimestampMap
			}
			switch key {
			case "LOCAL":
				if local {
					return LiveHeader{}, ErrLiveTimestampMap
				}
				ticks, err := parseTimestamp(raw, FormatWebVTT)
				if err != nil {
					return LiveHeader{}, ErrLiveTimestampMap
				}
				mapping.LocalTicks, local = ticks, true
			case "MPEGTS":
				if transport || !decimal(raw) {
					return LiveHeader{}, ErrLiveTimestampMap
				}
				pts, err := strconv.ParseUint(raw, 10, 33)
				if err != nil {
					return LiveHeader{}, ErrLiveTimestampMap
				}
				mapping.MPEGTS, transport = pts, true
			default:
				return LiveHeader{}, ErrLiveTimestampMap
			}
		}
		if !local || !transport {
			return LiveHeader{}, ErrLiveTimestampMap
		}
		header.TimestampMap = &mapping
	}
	return header, nil
}

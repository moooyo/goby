package subtitle

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

var (
	ErrLiveEpoch         = errors.New("unknown or invalid live subtitle generation")
	ErrLiveTrack         = errors.New("unknown live subtitle stream")
	ErrLiveConflict      = errors.New("conflicting live subtitle data")
	ErrLiveNotReady      = errors.New("live subtitle interval is not complete")
	ErrLiveWindowExpired = errors.New("live subtitle interval has expired")
	ErrLiveTimestampMap  = errors.New("live subtitle timestamp mapping requires an explicit epoch anchor")
)

// LiveJournalOptions bounds all retained generations together. MaxTracks applies
// to each generation. Zero fields select bounded defaults; no wall clock is used.
type LiveJournalOptions struct {
	MaxBytes  int64
	MaxCues   int
	MaxTracks int
	MaxEpochs int
}

const (
	liveEpochCharge = int64(128)
	liveTrackCharge = int64(256)
	liveCueCharge   = int64(512)
)

// LiveJournal keeps source-clock cues independently of client subtitle views.
// It does not authorize callers, receive network data or infer stream progress.
type LiveJournal struct {
	mu       sync.RWMutex
	options  LiveJournalOptions
	epochs   map[uint64]*liveEpoch
	highest  uint64
	minimum  uint64
	earliest int64
	bytes    int64
	cues     int
}

type liveEpoch struct {
	streams   []int
	tracks    map[int]*liveTrack
	watermark int64
	advanced  bool
	earliest  int64
	bytes     int64
}

type liveTrack struct {
	header   []string
	blocks   []metadataBlock
	metadata bool
	cues     []Cue
	byKey    map[liveCueKey]Cue
}

// WebVTT identifiers need not be globally unique. Repeated HLS copies are
// identified by the complete interval plus identifier, not the identifier alone.
type liveCueKey struct {
	start int64
	end   int64
	id    string
}

func NewLiveJournal(options LiveJournalOptions) (*LiveJournal, error) {
	if options.MaxBytes == 0 {
		options.MaxBytes = 8 << 20
	}
	if options.MaxCues == 0 {
		options.MaxCues = MaxCueCount
	}
	if options.MaxTracks == 0 {
		options.MaxTracks = 8
	}
	if options.MaxEpochs == 0 {
		options.MaxEpochs = 32
	}
	if options.MaxBytes < 1 || options.MaxBytes > 64<<20 || options.MaxCues < 1 || options.MaxCues > MaxCueCount ||
		options.MaxTracks < 1 || options.MaxTracks > 8 || options.MaxEpochs < 1 || options.MaxEpochs > 64 {
		return nil, fmt.Errorf("%w: invalid live journal limits", ErrLimitExceeded)
	}
	return &LiveJournal{options: options, epochs: make(map[uint64]*liveEpoch)}, nil
}

// BeginEpoch fixes the stream set for a monotonically increasing generation.
// Repeating the same generation and stream set is an inert retry.
func (j *LiveJournal) BeginEpoch(generation uint64, streamIndices []int) error {
	if generation == 0 || len(streamIndices) > j.options.MaxTracks {
		return ErrLiveEpoch
	}
	streams := append([]int(nil), streamIndices...)
	sort.Ints(streams)
	for index, stream := range streams {
		if stream < 0 || stream > 1<<31-1 || index > 0 && streams[index-1] == stream {
			return ErrLiveTrack
		}
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if old := j.epochs[generation]; old != nil {
		if len(old.streams) != len(streams) {
			return ErrLiveConflict
		}
		for index := range streams {
			if old.streams[index] != streams[index] {
				return ErrLiveConflict
			}
		}
		return nil
	}
	if generation <= j.highest || generation < j.minimum {
		return ErrLiveEpoch
	}
	charge := liveEpochCharge + int64(len(streams))*liveTrackCharge
	if len(j.epochs) >= j.options.MaxEpochs || charge > j.options.MaxBytes-j.bytes {
		return ErrLimitExceeded
	}
	epoch := &liveEpoch{streams: streams, tracks: make(map[int]*liveTrack), bytes: charge}
	if generation == j.minimum {
		epoch.earliest = j.earliest
	}
	for _, stream := range streams {
		epoch.tracks[stream] = &liveTrack{byKey: make(map[liveCueKey]Cue)}
	}
	j.epochs[generation], j.highest = epoch, generation
	j.bytes += charge
	return nil
}

// Append atomically adds normalized source-clock WebVTT or SRT cues. Imported
// timestamp maps must first be resolved and removed with MapLiveDocument.
// Identical interval/identifier copies are inert; changed text or settings
// conflict. A new cue cannot change an interval already sealed by Advance.
func (j *LiveJournal) Append(generation uint64, streamIndex int, document Document) error {
	header, blocks, err := liveDocumentParts(document)
	if err != nil {
		return err
	}
	if err := validateCues(document.Cues); err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	epoch, err := j.epoch(generation)
	if err != nil {
		return err
	}
	track := epoch.tracks[streamIndex]
	if track == nil {
		return ErrLiveTrack
	}
	charge := int64(0)
	if track.metadata {
		if !liveEqualStrings(header, track.header) {
			return ErrLiveConflict
		}
	} else {
		for _, line := range header {
			charge += int64(len(line)) + 24
		}
	}
	newBlocks := make([]metadataBlock, 0, len(blocks))
	knownBlocks := make(map[string]bool, len(track.blocks)+len(blocks))
	for _, block := range track.blocks {
		knownBlocks[block.text] = true
	}
	for _, block := range blocks {
		if knownBlocks[block.text] {
			continue
		}
		if len(track.cues) > 0 || epoch.advanced {
			return ErrLiveConflict
		}
		knownBlocks[block.text] = true
		newBlocks = append(newBlocks, block)
		charge += int64(len(block.text)) + 48
	}
	pending := make(map[liveCueKey]Cue)
	added := make([]Cue, 0, min(len(document.Cues), j.options.MaxCues))
	for _, cue := range document.Cues {
		key := liveCueKey{start: cue.StartTicks, end: cue.EndTicks, id: cue.Identifier}
		if old, exists := track.byKey[key]; exists {
			if old != cue {
				return ErrLiveConflict
			}
			continue
		}
		if old, exists := pending[key]; exists {
			if old != cue {
				return ErrLiveConflict
			}
			continue
		}
		if cue.EndTicks <= epoch.earliest || cue.StartTicks == cue.EndTicks {
			continue
		}
		if epoch.advanced && cue.StartTicks < epoch.watermark {
			return ErrLiveConflict
		}
		pending[key] = cue
		added = append(added, cue)
		charge += liveCueBytes(cue)
		if len(added) > j.options.MaxCues-j.cues || charge > j.options.MaxBytes-j.bytes {
			return ErrLimitExceeded
		}
	}
	if charge > j.options.MaxBytes-j.bytes {
		return ErrLimitExceeded
	}
	if !track.metadata {
		track.header = liveCloneStrings(header)
		track.metadata = true
	}
	for _, block := range newBlocks {
		track.blocks = append(track.blocks, metadataBlock{text: strings.Clone(block.text)})
	}
	for _, cue := range added {
		cue.Text, cue.Identifier, cue.Settings = strings.Clone(cue.Text), strings.Clone(cue.Identifier), strings.Clone(cue.Settings)
		track.cues = append(track.cues, cue)
		track.byKey[liveCueKey{start: cue.StartTicks, end: cue.EndTicks, id: cue.Identifier}] = cue
	}
	sort.SliceStable(track.cues, func(a, b int) bool {
		left, right := track.cues[a], track.cues[b]
		if left.StartTicks != right.StartTicks {
			return left.StartTicks < right.StartTicks
		}
		if left.EndTicks != right.EndTicks {
			return left.EndTicks < right.EndTicks
		}
		return left.Identifier < right.Identifier
	})
	j.cues += len(added)
	j.bytes += charge
	epoch.bytes += charge
	return nil
}

// Advance seals completeness through an observed source-media watermark. The
// caller must have drained the corresponding ordered subtitle input first; an
// elapsed timer or an empty pipe is not evidence that subtitles are complete.
func (j *LiveJournal) Advance(generation uint64, sourceWatermarkTicks int64) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	epoch, err := j.epoch(generation)
	if err != nil {
		return err
	}
	if sourceWatermarkTicks < epoch.earliest || sourceWatermarkTicks < epoch.watermark {
		return ErrInvalidRange
	}
	epoch.watermark, epoch.advanced = sourceWatermarkTicks, true
	return nil
}

// Window renders a detached view. Stream -1 means subtitles off. Negative
// offsets may require a later completeness watermark; positive offsets never
// recover data already removed by Prune. Crossing cues retain their full times.
func (j *LiveJournal) Window(generation uint64, streamIndex int, startTicks, endTicks, offsetTicks, clockDeltaTicks int64) (Result, error) {
	if startTicks < 0 || endTicks <= startTicks || offsetTicks < -MaxOffsetTicks || offsetTicks > MaxOffsetTicks ||
		clockDeltaTicks < -MaxOffsetTicks || clockDeltaTicks > MaxOffsetTicks {
		return Result{}, ErrInvalidRange
	}
	sourceStart, sourceEnd := startTicks, endTicks
	var err error
	if streamIndex != -1 {
		if sourceStart, err = liveAddTicks(startTicks, -offsetTicks); err != nil {
			return Result{}, err
		}
		if sourceEnd, err = liveAddTicks(endTicks, -offsetTicks); err != nil {
			return Result{}, err
		}
		sourceStart, sourceEnd = max(0, sourceStart), max(0, sourceEnd)
	}
	j.mu.RLock()
	defer j.mu.RUnlock()
	epoch, err := j.epoch(generation)
	if err != nil {
		return Result{}, err
	}
	if startTicks < epoch.earliest || sourceStart < epoch.earliest {
		return Result{}, ErrLiveWindowExpired
	}
	if !epoch.advanced || max(endTicks, sourceEnd) > epoch.watermark {
		return Result{}, ErrLiveNotReady
	}
	document := Document{Format: FormatWebVTT}
	if streamIndex != -1 {
		track := epoch.tracks[streamIndex]
		if track == nil {
			return Result{}, ErrLiveTrack
		}
		document.Cues, document.header, document.blocks = track.cues, track.header, track.blocks
	}
	return RenderHLSWindow(document, startTicks, endTicks, offsetTicks, clockDeltaTicks)
}

// Prune drops older generations and only fully expired cues in the earliest
// retained generation. The caller supplies actual retained-media coordinates.
func (j *LiveJournal) Prune(minGeneration uint64, earliestSourceTicks int64) error {
	if minGeneration == 0 || earliestSourceTicks < 0 {
		return ErrInvalidRange
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if minGeneration < j.minimum || minGeneration == j.minimum && earliestSourceTicks < j.earliest {
		return ErrInvalidRange
	}
	if epoch := j.epochs[minGeneration]; epoch != nil && earliestSourceTicks > 0 &&
		(!epoch.advanced || earliestSourceTicks > epoch.watermark) {
		return ErrLiveNotReady
	}
	for generation, epoch := range j.epochs {
		if generation < minGeneration {
			for _, track := range epoch.tracks {
				j.cues -= len(track.cues)
			}
			j.bytes -= epoch.bytes
			delete(j.epochs, generation)
			continue
		}
		if generation != minGeneration {
			continue
		}
		epoch.earliest = earliestSourceTicks
		for _, track := range epoch.tracks {
			retained := make([]Cue, 0)
			for _, cue := range track.cues {
				if cue.EndTicks > earliestSourceTicks {
					retained = append(retained, cue)
					continue
				}
				charge := liveCueBytes(cue)
				j.cues--
				j.bytes -= charge
				epoch.bytes -= charge
			}
			// Rebuild both containers so pruned high-water capacities do not
			// remain attached to an otherwise empty long-lived generation.
			track.cues = retained
			track.byKey = make(map[liveCueKey]Cue, len(retained))
			for _, cue := range retained {
				track.byKey[liveCueKey{start: cue.StartTicks, end: cue.EndTicks, id: cue.Identifier}] = cue
			}
		}
	}
	j.minimum, j.earliest = minGeneration, earliestSourceTicks
	return nil
}

func (j *LiveJournal) epoch(generation uint64) (*liveEpoch, error) {
	if generation > 0 && generation < j.minimum {
		return nil, ErrLiveWindowExpired
	}
	epoch := j.epochs[generation]
	if epoch == nil {
		return nil, ErrLiveEpoch
	}
	return epoch, nil
}

func liveCueBytes(cue Cue) int64 {
	return liveCueCharge + int64(len(cue.Text)+len(cue.Identifier)+len(cue.Settings))
}

func liveEqualStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func liveCloneStrings(values []string) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = strings.Clone(value)
	}
	return result
}

func liveDocumentParts(document Document) ([]string, []metadataBlock, error) {
	if document.Format != FormatWebVTT && document.Format != FormatSRT {
		return nil, nil, ErrUnsupportedFormat
	}
	if len(document.header) > MaxLineCount || len(document.blocks) > MaxLineCount {
		return nil, nil, ErrLimitExceeded
	}
	header := document.header
	if len(header) == 0 {
		header = []string{"WEBVTT"}
	}
	if !validSignature(header[0]) {
		return nil, nil, ErrInvalidDocument
	}
	total := 0
	for _, line := range header {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "X-TIMESTAMP-MAP") {
			return nil, nil, ErrLiveTimestampMap
		}
		if !utf8.ValidString(line) || strings.ContainsAny(line, "\x00\r\n") || blank(line) || strings.Contains(line, "-->") {
			return nil, nil, ErrInvalidDocument
		}
		if len(line) > MaxLineBytes {
			return nil, nil, ErrLimitExceeded
		}
		total += len(line) + 1
		if total > MaxInputBytes {
			return nil, nil, ErrLimitExceeded
		}
	}
	blocks := make([]metadataBlock, 0, len(document.blocks))
	for _, block := range document.blocks {
		if block.beforeCue < 0 || block.beforeCue > len(document.Cues) || !utf8.ValidString(block.text) || strings.ContainsAny(block.text, "\x00\r") {
			return nil, nil, ErrInvalidDocument
		}
		total += len(block.text) + 2
		if total > MaxInputBytes {
			return nil, nil, ErrLimitExceeded
		}
		lines, err := linesOf(block.text)
		if err != nil {
			return nil, nil, err
		}
		if len(lines) == 0 || !isMetadata(lines[0]) {
			return nil, nil, ErrInvalidDocument
		}
		kind := strings.TrimRight(lines[0], " \t")
		if kind != "STYLE" && kind != "REGION" {
			continue
		}
		if block.beforeCue != 0 || strings.Contains(block.text, "-->") {
			return nil, nil, ErrInvalidDocument
		}
		blocks = append(blocks, metadataBlock{text: block.text})
	}
	return header, blocks, nil
}

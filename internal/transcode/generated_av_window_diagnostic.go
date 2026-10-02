package transcode

import (
	"context"
	"os"
)

// GeneratedAVDiagnosticBaseline is supplied by a genuine scoped manager bridge.
// A completed Record is a production receipt, never A/V closure. On success the
// bridge transfers all listed ReadHandles to the diagnostic leaf; on failure
// the bridge closes every reader it opened. The leaf never closes Source.
type GeneratedAVDiagnosticBaseline struct {
	Record          Record
	MuxClocks       [MaxHLSRenditions]HLSMuxClock
	Playlists       [MaxHLSRenditions]MediaPlaylist
	PlaylistHandles [MaxHLSRenditions]*ReadHandle   `json:"-"`
	MediaHandles    [MaxHLSRenditions][]*ReadHandle `json:"-"`
}

// GeneratedAVWindowDiagnosticOptions carries trusted caller capabilities, not
// HTTP parameters. The original negotiated plan and its entire ladder are
// retained. Only explicit generic finite-window coordinates are cloned into a
// diagnostic plan. AcquireProbe uses the caller's existing bounded probe lane.
type GeneratedAVWindowDiagnosticOptions struct {
	FFmpegPath, FFprobePath, Directory string
	NegotiatedPlan                     Plan
	StartTicks, EndTicks               int64
	StartNumber, Threads               int
	AcquireProbe                       func(context.Context) (func(), error)                                        `json:"-"`
	Baseline                           func(context.Context, *os.File, Plan) (GeneratedAVDiagnosticBaseline, error) `json:"-"`
}

// GeneratedAVDiagnosticCut keeps raw coded transport, demuxed packets and
// independently decoded effective frames/PCM separate for each physical cut.
type GeneratedAVDiagnosticCut struct {
	Name          string
	Number        int64
	DurationTicks int64
	Identity      string
	Transport     GeneratedAVTransportDiagnostic
	Packets       GeneratedSegmentBounds
	Effective     GeneratedAVEffectiveDecodeDiagnostic
	Observation   GeneratedAVDecodedObservation
	PCM           GeneratedAVPCMDiagnostic
}

type GeneratedAVDiagnosticRendition struct {
	Variant               int
	Playlist              MediaPlaylist
	Cuts                  []GeneratedAVDiagnosticCut
	ContinuousEffective   GeneratedAVEffectiveDecodeDiagnostic
	ContinuousObservation GeneratedAVDecodedObservation
	ContinuousPCM         GeneratedAVPCMDiagnostic
}

// GeneratedAVDiagnosticRun keeps manager production and governed observation
// receipts distinct. BaselineManagerCompleted cannot claim a media-governor
// lease, source EOF, ProductionSealSafe, A/V closure or native playback.
type GeneratedAVDiagnosticRun struct {
	Plan                      Plan
	Arguments                 []string
	BaselineManagerCompleted  bool
	Record                    Record
	MuxClocks                 [MaxHLSRenditions]HLSMuxClock
	VideoInput                [MaxHLSRenditions]GeneratedInputEvidence
	AudioInput                [MaxHLSRenditions]RawGeneratedAudioInputEvidence
	PreMux                    []GeneratedAVPreMuxDiagnostic
	Renditions                []GeneratedAVDiagnosticRendition
	ObserversJoined           bool
	SourceIdentityUnchanged   bool
	OutputIdentitiesUnchanged bool
}

type GeneratedAVDiagnosticComparison struct {
	MediaBytesEqual           bool
	PacketClocksEqual         bool
	PhysicalCutsEqual         bool
	NegotiatedLadderPreserved bool
}

// GeneratedAVWindowDiagnostic never qualifies playback or production. Complete
// means the bounded diagnostic observations finished; actual cut/priming facts
// can still disagree with the requested interval or with another rendition.
type GeneratedAVWindowDiagnostic struct {
	Qualified           bool
	Complete            bool
	NativeClockComplete bool
	NegotiatedPlan      Plan
	WindowPlan          Plan
	SourceCertificate   GeneratedAVSourceCertificate
	SourceEffective     GeneratedAVEffectiveDecodeDiagnostic
	SourcePCM           GeneratedAVPCMDiagnostic
	Baseline, Observer  GeneratedAVDiagnosticRun
	ObserverDelta       []string
	Comparison          GeneratedAVDiagnosticComparison
}

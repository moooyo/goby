package transcode

import (
	"context"
	"os"
)

type GeneratedAVPCMCaptureRole string

const (
	GeneratedAVPCMCaptureSource GeneratedAVPCMCaptureRole = "source"
	GeneratedAVPCMCaptureCut    GeneratedAVPCMCaptureRole = "cut"
	GeneratedAVPCMCaptureGroup  GeneratedAVPCMCaptureRole = "group"
)

// GeneratedAVPCMCaptureOptions contains trusted diagnostic capabilities, not
// HTTP arguments. Channels must come from independently observed native stream
// facts and match the selected 48 kHz source configuration. The command adds no
// resampling/remixing -ar/-ac options. Capture transfers ownership at entry and
// must be a new empty private 0600 O_EXCL descriptor distinct from every input.
// An invalid Capture sharing a borrowed pointer/valid OS descriptor is rejected
// before transfer and remains borrowed; every other supplied artifact is consumed.
type GeneratedAVPCMCaptureOptions struct {
	FFmpegPath           string
	SourceCertificate    GeneratedAVSourceCertificate
	ExpectedSourceSHA256 [32]byte
	Channels             int
	Role                 GeneratedAVPCMCaptureRole
	AcquireProbe         func(context.Context) (func(), error) `json:"-"`
	Capture              *os.File                              `json:"-"`
}

// GeneratedAVPCMCaptureDiagnostic observes full actually emitted s16le PCM.
// PCM.SHA256 hashes the emitted bytes also written to the private capture; a
// later reader must independently hash the closed capture before comparison.
// SourceSHA256 is the complete encoded source, separate from ordered copied
// InputSHA256 parts and from PCM.SHA256. PCM carries no returned native clock.
// Complete proves joined streaming/capture and input fences, never audible trim,
// decoded source origin/content, source EOF or playback qualification.
type GeneratedAVPCMCaptureDiagnostic struct {
	Qualified, Complete, CaptureWritten                      bool
	NativeClockComplete, DecodedOriginComplete, ContentBound bool
	Stage                                                    string
	Role                                                     GeneratedAVPCMCaptureRole
	LimitBytes                                               int64
	PCM                                                      GeneratedAVPCMDiagnostic
	SourceBytes, InputBytes                                  int64
	SourceSHA256                                             [32]byte
	InputSHA256                                              [][32]byte
	SourceIdentity                                           string
	InputIdentities                                          []string
	SourceIdentityUnchanged, InputIdentitiesUnchanged        bool
}

func generatedAVPCMCaptureLimit(role GeneratedAVPCMCaptureRole, parts int) (int64, error) {
	switch role {
	case GeneratedAVPCMCaptureSource:
		if parts == 1 {
			return generatedAVPCMBytes, nil
		}
	case GeneratedAVPCMCaptureCut:
		if parts == 1 {
			return 8 << 20, nil
		}
	case GeneratedAVPCMCaptureGroup:
		if parts >= 2 && parts <= 4 {
			return 8 << 20, nil
		}
	}
	return 0, ErrInvalidOptions
}

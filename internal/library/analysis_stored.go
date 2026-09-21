package library

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"

	"github.com/moooyo/goby/internal/introdetect"
)

// Historical facts deliberately contain no matcher metrics. An observation
// from a retired algorithm must never acquire current measurement semantics.
type AnalysisStoredIntervalFacts struct {
	StartTicks int64
	EndTicks   int64
}

type AnalysisStoredSupportFacts struct {
	EpisodeKey, SourceKey, ContentIdentity string
	Interval                               AnalysisStoredIntervalFacts
}

type AnalysisStoredCandidateFacts struct {
	Interval AnalysisStoredIntervalFacts
	GroupID  string
	Status   string
	Reasons  []string
	Support  []AnalysisStoredSupportFacts
}

type AnalysisStoredEpisodeFacts struct {
	EpisodeKey, SourceKey, ContentIdentity string
	Status                                 string
	Reasons                                []string
	Candidates                             []AnalysisStoredCandidateFacts
}

type AnalysisStoredResultFacts struct {
	Version string
	Episode AnalysisStoredEpisodeFacts
	Reason  string
}

type AnalysisStoredAuditFacts struct {
	Result   *AnalysisStoredResultFacts
	Decision *AnalysisDecision
}

func analysisV1ResultFacts(value analysisStoredResultV1) AnalysisStoredResultFacts {
	episode := value.Episode
	result := AnalysisStoredResultFacts{Version: value.Version, Reason: value.Reason,
		Episode: AnalysisStoredEpisodeFacts{EpisodeKey: episode.EpisodeKey, SourceKey: episode.SourceKey,
			ContentIdentity: episode.ContentIdentity, Status: episode.Status, Reasons: episode.Reasons,
			Candidates: make([]AnalysisStoredCandidateFacts, 0, len(episode.Candidates))}}
	for _, candidate := range episode.Candidates {
		fact := AnalysisStoredCandidateFacts{Interval: AnalysisStoredIntervalFacts(candidate.Interval),
			GroupID: candidate.GroupID, Status: candidate.Status, Reasons: candidate.Reasons,
			Support: make([]AnalysisStoredSupportFacts, 0, len(candidate.Support))}
		for _, support := range candidate.Support {
			fact.Support = append(fact.Support, AnalysisStoredSupportFacts{support.EpisodeKey, support.SourceKey,
				support.ContentIdentity, AnalysisStoredIntervalFacts(support.Interval)})
		}
		result.Episode.Candidates = append(result.Episode.Candidates, fact)
	}
	return result
}

func analysisV2ResultFacts(value analysisStoredResultV2) AnalysisStoredResultFacts {
	episode := value.Episode
	result := AnalysisStoredResultFacts{Version: value.Version, Reason: value.Reason,
		Episode: AnalysisStoredEpisodeFacts{EpisodeKey: episode.EpisodeKey, SourceKey: episode.SourceKey,
			ContentIdentity: episode.ContentIdentity, Status: episode.Status, Reasons: episode.Reasons,
			Candidates: make([]AnalysisStoredCandidateFacts, 0, len(episode.Candidates))}}
	for _, candidate := range episode.Candidates {
		fact := AnalysisStoredCandidateFacts{Interval: AnalysisStoredIntervalFacts(candidate.Interval),
			GroupID: candidate.GroupID, Status: candidate.Status, Reasons: candidate.Reasons,
			Support: make([]AnalysisStoredSupportFacts, 0, len(candidate.Support))}
		for _, support := range candidate.Support {
			fact.Support = append(fact.Support, AnalysisStoredSupportFacts{support.EpisodeKey, support.SourceKey,
				support.ContentIdentity, AnalysisStoredIntervalFacts(support.Interval)})
		}
		result.Episode.Candidates = append(result.Episode.Candidates, fact)
	}
	return result
}

func analysisCurrentResultFacts(value AnalysisStoredResult) AnalysisStoredResultFacts {
	episode := value.Episode
	result := AnalysisStoredResultFacts{Version: value.Version, Reason: value.Reason,
		Episode: AnalysisStoredEpisodeFacts{EpisodeKey: episode.EpisodeKey, SourceKey: episode.SourceKey,
			ContentIdentity: episode.ContentIdentity, Status: string(episode.Status), Reasons: []string{},
			Candidates: make([]AnalysisStoredCandidateFacts, 0, len(episode.Candidates))}}
	for _, reason := range episode.Reasons {
		result.Episode.Reasons = append(result.Episode.Reasons, string(reason))
	}
	for _, candidate := range episode.Candidates {
		fact := AnalysisStoredCandidateFacts{Interval: AnalysisStoredIntervalFacts(candidate.Interval),
			GroupID: candidate.GroupID, Status: string(candidate.Status), Reasons: []string{},
			Support: make([]AnalysisStoredSupportFacts, 0, len(candidate.Support))}
		for _, reason := range candidate.Reasons {
			fact.Reasons = append(fact.Reasons, string(reason))
		}
		for _, support := range candidate.Support {
			fact.Support = append(fact.Support, AnalysisStoredSupportFacts{support.EpisodeKey, support.SourceKey,
				support.ContentIdentity, AnalysisStoredIntervalFacts(support.Interval)})
		}
		result.Episode.Candidates = append(result.Episode.Candidates, fact)
	}
	return result
}

func decodeAnalysisStoredResult(raw []byte) (AnalysisStoredResultFacts, *AnalysisStoredResult, error) {
	var version struct{ Version string }
	if len(raw) > 131072 || json.Unmarshal(raw, &version) != nil {
		return AnalysisStoredResultFacts{}, nil, ErrInvalidInput
	}
	if version.Version == analysisStoredDetectorVersionV1 {
		var value analysisStoredResultV1
		if analysisStrictJSON(raw, &value) != nil || validateAnalysisStoredResultV1(value) != nil {
			return AnalysisStoredResultFacts{}, nil, ErrInvalidInput
		}
		return analysisV1ResultFacts(value), nil, nil
	}
	if version.Version == analysisStoredDetectorVersionV2 {
		var value analysisStoredResultV2
		if analysisStrictJSON(raw, &value) != nil || validateAnalysisStoredResultV2(value) != nil {
			return AnalysisStoredResultFacts{}, nil, ErrInvalidInput
		}
		return analysisV2ResultFacts(value), nil, nil
	}
	if version.Version != introdetect.Version {
		return AnalysisStoredResultFacts{}, nil, ErrInvalidInput
	}
	var value AnalysisStoredResult
	if analysisStrictJSON(raw, &value) != nil || validateAnalysisStoredResult(value) != nil {
		return AnalysisStoredResultFacts{}, nil, ErrInvalidInput
	}
	return analysisCurrentResultFacts(value), &value, nil
}

func analysisStoredFactsMatch(value AnalysisStoredResultFacts, status string, start, end *int64) bool {
	if value.Episode.Status != status {
		return false
	}
	var expectedStart, expectedEnd *int64
	if len(value.Episode.Candidates) != 0 {
		interval := value.Episode.Candidates[0].Interval
		expectedStart, expectedEnd = &interval.StartTicks, &interval.EndTicks
	}
	return reflect.DeepEqual(start, expectedStart) && reflect.DeepEqual(end, expectedEnd)
}

// ReadStoredAnalysisResult validates known historical formats without granting
// current execution or publication authority and without synthesizing metrics.
func ReadStoredAnalysisResult(raw []byte, status string, start, end *int64) (AnalysisStoredResultFacts, error) {
	value, _, err := decodeAnalysisStoredResult(raw)
	if err != nil || !analysisStoredFactsMatch(value, status, start, end) {
		return AnalysisStoredResultFacts{}, ErrInvalidInput
	}
	return value, nil
}

// analysisStoredAuditObject retains raw nested result bytes for version dispatch.
// The complete outer spelling, presence, duplicate and trailing-input contract
// is checked here; each non-null nested object gets its own strict decoder.
func analysisStoredAuditObject(raw []byte) (map[string]json.RawMessage, error) {
	if len(raw) > 131072 {
		return nil, ErrInvalidInput
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, ErrInvalidInput
	}
	fields := make(map[string]json.RawMessage, 2)
	for decoder.More() {
		token, err := decoder.Token()
		key, valid := token.(string)
		if err != nil || !valid || key != "Result" && key != "Decision" || fields[key] != nil {
			return nil, ErrInvalidInput
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, ErrInvalidInput
		}
		fields[key] = value
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') || len(fields) != 2 {
		return nil, ErrInvalidInput
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, ErrInvalidInput
	}
	return fields, nil
}

// ReadStoredAnalysisAudit preserves known historical result facts and explicit
// decisions without promoting either into current matcher evidence.
func ReadStoredAnalysisAudit(raw []byte, action string) (AnalysisStoredAuditFacts, error) {
	fields, err := analysisStoredAuditObject(raw)
	if err != nil {
		return AnalysisStoredAuditFacts{}, err
	}
	var value AnalysisStoredAuditFacts
	if !bytes.Equal(bytes.TrimSpace(fields["Result"]), []byte("null")) {
		result, _, err := decodeAnalysisStoredResult(fields["Result"])
		if err != nil {
			return AnalysisStoredAuditFacts{}, err
		}
		value.Result = &result
	}
	if !bytes.Equal(bytes.TrimSpace(fields["Decision"]), []byte("null")) {
		var decision AnalysisDecision
		if analysisStrictJSON(fields["Decision"], &decision) != nil {
			return AnalysisStoredAuditFacts{}, ErrInvalidInput
		}
		value.Decision = &decision
	}
	switch action {
	case "qualified", "review", "no_result":
		if value.Result == nil || value.Decision != nil || value.Result.Episode.Status != action {
			return AnalysisStoredAuditFacts{}, ErrInvalidInput
		}
	case "accept", "reject", "reset":
		if value.Decision == nil || value.Decision.Action != action || !analysisOpaque(value.Decision.SourceRevision, 256) {
			return AnalysisStoredAuditFacts{}, ErrInvalidInput
		}
		if _, err := analysisRevision(value.Decision.Revision); err != nil {
			return AnalysisStoredAuditFacts{}, err
		}
		if _, err := analysisRevision(value.Decision.ManualRevision); err != nil {
			return AnalysisStoredAuditFacts{}, err
		}
	default:
		return AnalysisStoredAuditFacts{}, ErrInvalidInput
	}
	return value, nil
}

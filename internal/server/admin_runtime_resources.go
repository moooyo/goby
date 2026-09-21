package server

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/library"
)

const (
	originalResourceCurrentLimit    = 64
	originalResourceCompletionLimit = 128
	originalResourceMaxSequence     = ^uint64(0) - 1
)

type originalResourceRecord struct {
	leaseID                  string
	sequence                 uint64
	completionSequence       uint64
	itemID, mediaSourceID    string
	started, completed       int64
	active, identifiersValid bool
}

type originalResourceLeaseDTO struct {
	LeaseID            string `json:"LeaseId"`
	Sequence           string
	CompletionSequence string
	ItemID             string `json:"ItemId"`
	MediaSourceID      string `json:"MediaSourceId"`
	StartedUnixNano    string
	CompletedUnixNano  string
	Active             bool
}

type originalResourcesDTO struct {
	InstanceID                string `json:"InstanceId"`
	ActiveCount               int
	CurrentLimit              int
	CompletionLimit           int
	CurrentCapacityDropped    int
	CompletionCapacityDropped string
	NextLeaseSequence         string
	NextCompletionSequence    string
	OldestCompletionSequence  string
	Current                   []originalResourceLeaseDTO
	Completed                 []originalResourceLeaseDTO
}

type adminRuntimeResourcesDTO struct {
	ScanEvidence        library.ScanEvidenceStatus
	StorageObservations library.StorageObservationStatus
	OriginalStreams     originalResourcesDTO
	DatabasePool        databasePoolResourcesDTO
}

type databasePoolResourcesDTO struct {
	MaxConns                    int32
	TotalConns                  int32
	IdleConns                   int32
	AcquiredConns               int32
	ConstructingConns           int32
	AcquireCount                string
	AcquireDurationNanoseconds  string
	EmptyAcquireCount           string
	EmptyAcquireWaitNanoseconds string
	CanceledAcquireCount        string
}

// One in-memory Stat snapshot reports successful acquisitions and their wait
// durations separately from canceled acquisitions. It never borrows a pool
// connection or queries PostgreSQL. Empty waits include connection construction.
func databasePoolResources(snapshot *pgxpool.Stat) databasePoolResourcesDTO {
	return databasePoolResourcesDTO{
		MaxConns: snapshot.MaxConns(), TotalConns: snapshot.TotalConns(), IdleConns: snapshot.IdleConns(),
		AcquiredConns: snapshot.AcquiredConns(), ConstructingConns: snapshot.ConstructingConns(),
		AcquireCount:                strconv.FormatInt(snapshot.AcquireCount(), 10),
		AcquireDurationNanoseconds:  strconv.FormatInt(snapshot.AcquireDuration().Nanoseconds(), 10),
		EmptyAcquireCount:           strconv.FormatInt(snapshot.EmptyAcquireCount(), 10),
		EmptyAcquireWaitNanoseconds: strconv.FormatInt(snapshot.EmptyAcquireWaitTime().Nanoseconds(), 10),
		CanceledAcquireCount:        strconv.FormatInt(snapshot.CanceledAcquireCount(), 10),
	}
}

func newOriginalResourceInstanceID() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", library.ErrUnavailable
	}
	return hex.EncodeToString(nonce[:]), nil
}

// Domain item/source IDs are bounded ASCII identifiers. Invalid diagnostic
// metadata never changes stream admission or source-retirement matching, but a
// snapshot containing it must fail rather than expose unbounded/path-like data.
func originalResourceIdentifier(value string) bool {
	if len(value) > 128 {
		return false
	}
	for _, character := range value {
		if character != '-' && character != '_' && !(character >= '0' && character <= '9') &&
			!(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') {
			return false
		}
	}
	return true
}

// The caller holds runtime.mu; no principal or request metadata is retained.
func (runtime *originalStreamRuntime) nextResourceLease(itemID, sourceID string) (originalResourceRecord, error) {
	if runtime.resourceErr != nil || runtime.leaseSequence >= originalResourceMaxSequence {
		return originalResourceRecord{}, library.ErrUnavailable
	}
	runtime.leaseSequence++
	record := originalResourceRecord{leaseID: runtime.instanceID + "-" + strconv.FormatUint(runtime.leaseSequence, 10),
		sequence: runtime.leaseSequence, started: time.Now().UnixNano(), active: true,
		identifiersValid: originalResourceIdentifier(itemID) && originalResourceIdentifier(sourceID)}
	if record.identifiersValid {
		// The bounded history must not retain a larger caller-owned backing string.
		record.itemID, record.mediaSourceID = strings.Clone(itemID), strings.Clone(sourceID)
	}
	return record, nil
}

// Only the existing once-protected leave callback appends a completion.
func (runtime *originalStreamRuntime) completeResourceLease(record originalResourceRecord) {
	runtime.completionSequence++
	record.completionSequence = runtime.completionSequence
	record.completed, record.active = time.Now().UnixNano(), false
	runtime.completed[runtime.completedHead] = record
	runtime.completedHead = (runtime.completedHead + 1) % originalResourceCompletionLimit
	if runtime.completedCount < originalResourceCompletionLimit {
		runtime.completedCount++
	}
}

func originalResourceLease(record originalResourceRecord) originalResourceLeaseDTO {
	return originalResourceLeaseDTO{LeaseID: record.leaseID, Sequence: strconv.FormatUint(record.sequence, 10),
		CompletionSequence: strconv.FormatUint(record.completionSequence, 10), ItemID: record.itemID,
		MediaSourceID: record.mediaSourceID, StartedUnixNano: strconv.FormatInt(record.started, 10),
		CompletedUnixNano: strconv.FormatInt(record.completed, 10), Active: record.active}
}

func (runtime *originalStreamRuntime) resourceSnapshot() (originalResourcesDTO, error) {
	if runtime == nil {
		return originalResourcesDTO{}, library.ErrUnavailable
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.resourceErr != nil {
		return originalResourcesDTO{}, runtime.resourceErr
	}
	result := originalResourcesDTO{InstanceID: runtime.instanceID, ActiveCount: len(runtime.sources),
		CurrentLimit: originalResourceCurrentLimit, CompletionLimit: originalResourceCompletionLimit,
		Current:                   make([]originalResourceLeaseDTO, 0, originalResourceCurrentLimit),
		Completed:                 make([]originalResourceLeaseDTO, 0, runtime.completedCount),
		CompletionCapacityDropped: strconv.FormatUint(runtime.completionSequence-uint64(runtime.completedCount), 10),
		NextLeaseSequence:         strconv.FormatUint(runtime.leaseSequence+1, 10),
		NextCompletionSequence:    strconv.FormatUint(runtime.completionSequence+1, 10), OldestCompletionSequence: "0"}
	current := make([]originalResourceRecord, 0, originalResourceCurrentLimit)
	for lease := range runtime.sources {
		if !lease.resource.identifiersValid {
			return originalResourcesDTO{}, library.ErrUnavailable
		}
		if len(current) < originalResourceCurrentLimit {
			current = append(current, lease.resource)
			continue
		}
		// Keep the oldest 64 active leases without allocating for omitted rows.
		latest := 0
		for index := 1; index < len(current); index++ {
			if current[index].sequence > current[latest].sequence {
				latest = index
			}
		}
		if lease.resource.sequence < current[latest].sequence {
			current[latest] = lease.resource
		}
	}
	sort.Slice(current, func(i, j int) bool { return current[i].sequence < current[j].sequence })
	for _, record := range current {
		result.Current = append(result.Current, originalResourceLease(record))
	}
	result.CurrentCapacityDropped = result.ActiveCount - len(result.Current)
	for offset := 0; offset < runtime.completedCount; offset++ {
		index := (runtime.completedHead - runtime.completedCount + offset + originalResourceCompletionLimit) % originalResourceCompletionLimit
		record := runtime.completed[index]
		if !record.identifiersValid {
			return originalResourcesDTO{}, library.ErrUnavailable
		}
		result.Completed = append(result.Completed, originalResourceLease(record))
	}
	if len(result.Completed) != 0 {
		result.OldestCompletionSequence = result.Completed[0].CompletionSequence
	}
	return result, nil
}

func (s *Server) registerAdminRuntimeResourcesRoutes(mux *http.ServeMux) {
	authorized := s.requireAdmin(s.adminRuntimeResources)
	mux.HandleFunc("GET /admin/v1/runtime/resources", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		authorized(w, r)
	})
}

func (s *Server) adminRuntimeResources(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		apiError(w, r, http.StatusBadRequest, "invalid_query", "This resource does not accept query parameters.")
		return
	}
	originals, err := s.originals.resourceSnapshot()
	if err != nil || s.library == nil || s.db == nil {
		apiError(w, r, http.StatusServiceUnavailable, "runtime_resources_unavailable", "The runtime resource snapshot is unavailable.")
		return
	}
	jsonResponse(w, http.StatusOK, adminRuntimeResourcesDTO{ScanEvidence: s.library.ScanEvidenceStatus(),
		StorageObservations: library.StorageObservationSnapshot(), OriginalStreams: originals,
		DatabasePool: databasePoolResources(s.db.Stat())})
}

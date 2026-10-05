//go:build linux

package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// This opt-in common overlay only observes the existing gate and client spans.
// It does not warm a statement, alter a transaction end, or hold a barrier.
func hlsStopDiagnosticEnabled() bool { return os.Getenv("GOBY_HTTP_STOP_DIAGNOSTIC") == "1" }

type hlsStopDiagnosticPrepareContextKey struct{}
type hlsStopDiagnosticClientContextKey struct{}

type hlsStopDiagnosticPrepareEvidence struct {
	NameKind        string `json:"name_kind"`
	AlreadyPrepared bool   `json:"already_prepared"`
	Mode            string `json:"query_exec_mode"`
}

type hlsStopDiagnosticQueryDetail struct {
	Fingerprint string
	Prepare     *hlsStopDiagnosticPrepareEvidence
}

func hlsStopDiagnosticQueryDetailFor(request *hlsPhaseTimingRequest, sql string) *hlsStopDiagnosticQueryDetail {
	if !hlsStopDiagnosticEnabled() || request == nil || request.group != "stopped" {
		return nil
	}
	digest := sha256.Sum256([]byte(strings.Join(strings.Fields(sql), " ")))
	return &hlsStopDiagnosticQueryDetail{Fingerprint: hex.EncodeToString(digest[:])}
}

// pgx inspects the outer pool tracer for PrepareTracer. Defining these methods
// on only the nested priority tracer would miss unnamed cache_describe Prepare.
var _ pgx.PrepareTracer = (*httpGetStopRemeasureTracer)(nil)

func (*httpGetStopRemeasureTracer) TracePrepareStart(ctx context.Context, connection *pgx.Conn, data pgx.TracePrepareStartData) context.Context {
	request := hlsPhaseTimingFor(ctx)
	detail := hlsStopDiagnosticQueryDetailFor(request, data.SQL)
	if detail == nil {
		return ctx
	}
	kind := "named"
	if data.Name == "" {
		kind = "unnamed"
	}
	detail.Prepare = &hlsStopDiagnosticPrepareEvidence{NameKind: kind, Mode: connection.Config().DefaultQueryExecMode.String()}
	return context.WithValue(ctx, hlsStopDiagnosticPrepareContextKey{}, &hlsPhaseTimingSpan{
		request: request, started: time.Now(), category: hlsPhaseTimingCategory(data.SQL),
		pid: connection.PgConn().PID(), diagnostic: detail})
}

func (*httpGetStopRemeasureTracer) TracePrepareEnd(ctx context.Context, _ *pgx.Conn, data pgx.TracePrepareEndData) {
	if span, _ := ctx.Value(hlsStopDiagnosticPrepareContextKey{}).(*hlsPhaseTimingSpan); span != nil {
		span.diagnostic.Prepare.AlreadyPrepared = data.AlreadyPrepared
		span.request.record("prepare", span.category, span.started, span.pid, 0, data.Err != nil, span.diagnostic)
	}
}

type hlsStopDiagnosticGateEvidence struct {
	at       time.Time
	callback string
}

func hlsStopDiagnosticMarkGate(counts *hlsPriorityProfileCounts, callback string) {
	if hlsStopDiagnosticEnabled() {
		counts.stopDiagnosticGate.Store(&hlsStopDiagnosticGateEvidence{at: time.Now(), callback: callback})
	}
}

type hlsStopDiagnosticClientEvidence struct {
	receivedAt, dispatchedAt, completedAt time.Time
}

func hlsStopDiagnosticBegin(t *testing.T) *hlsStopDiagnosticClientEvidence {
	t.Helper()
	if !hlsStopDiagnosticEnabled() {
		return nil
	}
	if !hlsPhaseTimingEnabled() {
		t.Fatal("Stop diagnostics require the original phase timing recorder")
	}
	return &hlsStopDiagnosticClientEvidence{}
}

func hlsStopDiagnosticGateReceived(evidence *hlsStopDiagnosticClientEvidence) {
	if evidence != nil {
		evidence.receivedAt = time.Now()
	}
}

func hlsStopDiagnosticClientContext(ctx context.Context, evidence *hlsStopDiagnosticClientEvidence) context.Context {
	if evidence == nil {
		return ctx
	}
	return context.WithValue(ctx, hlsStopDiagnosticClientContextKey{}, evidence)
}

func hlsStopDiagnosticClientStart(ctx context.Context, phase string, at time.Time) {
	if phase == "stopped" {
		if evidence, _ := ctx.Value(hlsStopDiagnosticClientContextKey{}).(*hlsStopDiagnosticClientEvidence); evidence != nil {
			evidence.dispatchedAt = at
		}
	}
}

func hlsStopDiagnosticClientEnd(ctx context.Context, phase string, at time.Time) {
	if phase == "stopped" {
		if evidence, _ := ctx.Value(hlsStopDiagnosticClientContextKey{}).(*hlsStopDiagnosticClientEvidence); evidence != nil {
			evidence.completedAt = at
		}
	}
}

func hlsStopDiagnosticExport(t *testing.T, record map[string]any, current *hlsPriorityProfileCase, evidence *hlsStopDiagnosticClientEvidence) {
	t.Helper()
	if evidence == nil {
		return
	}
	gate := current.stoppingGET.stopDiagnosticGate.Load()
	if gate == nil || gate.at.IsZero() || evidence.receivedAt.Before(gate.at) || evidence.dispatchedAt.Before(evidence.receivedAt) || evidence.completedAt.Before(evidence.dispatchedAt) {
		t.Fatal("Stop diagnostic anchors were incomplete or out of order")
	}
	record["stop_diagnostic"] = map[string]any{
		"schema_version": 1, "gate_callback": gate.callback,
		"gate_callback_observed_at": gate.at.UTC(), "gate_received_at": evidence.receivedAt.UTC(),
		"client_dispatch_at": evidence.dispatchedAt.UTC(), "client_body_close_completed_at": evidence.completedAt.UTC(),
		"gate_to_receive_ns":               evidence.receivedAt.Sub(gate.at).Nanoseconds(),
		"receive_to_client_dispatch_ns":    evidence.dispatchedAt.Sub(evidence.receivedAt).Nanoseconds(),
		"client_dispatch_to_body_close_ns": evidence.completedAt.Sub(evidence.dispatchedAt).Nanoseconds(),
		"contract":                         "The gate anchor is immediately before the original channel close; query_start and batch_result_completion are distinct callbacks, neither a held-lock barrier. Dispatch is the existing client.Do span start and completion is its existing post-Body.Close elapsed observation. Prepare is a nested client span, not isolated PostgreSQL planning; no SQL text or arguments are exported.",
	}
}

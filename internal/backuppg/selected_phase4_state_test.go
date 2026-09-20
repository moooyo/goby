package backuppg

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
)

type phase4RuntimeTx struct {
	pgx.Tx
	raw     []byte
	err     error
	queries int
}

func (tx *phase4RuntimeTx) QueryRow(context.Context, string, ...any) pgx.Row {
	tx.queries++
	return phase4RuntimeRow{tx.raw, tx.err}
}

type phase4RuntimeRow struct {
	raw []byte
	err error
}

func (row phase4RuntimeRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	*dest[0].(*[]byte) = row.raw
	return nil
}

func TestValidateSelectedPhase4StatePreservesVersionAndStrictRuntimeBoundary(t *testing.T) {
	for _, version := range []int64{43, 44, 45, 46, 47, 48} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := validateSelectedPhase4State(ctx, nil, version); !errors.Is(err, context.Canceled) {
			t.Fatalf("schema%d lost cancellation: %v", version, err)
		}
		want := error(nil)
		if version >= 47 {
			want = ErrDatabase
		}
		if err := validateSelectedPhase4State(context.Background(), nil, version); !errors.Is(err, want) {
			t.Fatalf("schema%d crossed the runtime migration boundary: %v", version, err)
		}
	}
	for _, fixture := range []struct {
		name, network  string
		queryErr, want error
	}{
		{name: "neutral", network: `null`},
		{name: "explicit", network: `{"BindHost":"127.0.0.1","HttpPort":10096}`},
		{name: "sql_valid_invalid_ip", network: `{"BindHost":"999.0.0.1","HttpPort":10096}`, want: ErrSchema},
		{name: "unknown_nested_field", network: `{"BindHost":null,"HttpPort":10096,"Active":true}`, want: ErrSchema},
		{name: "read_failure", queryErr: errors.New("unavailable"), want: ErrDatabase},
		{name: "driver_deadline", queryErr: fmt.Errorf("driver: %w", context.DeadlineExceeded), want: context.DeadlineExceeded},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			raw := []byte(`{"Network":` + fixture.network + `,"Hardware":null,"Threads":null,"H264":null,"HEVC":null,"SoftwareToneMapping":null,"VulkanToneMapping":null}`)
			tx := &phase4RuntimeTx{raw: raw, err: fixture.queryErr}
			if err := validateSelectedPhase4State(context.Background(), tx, 47); !errors.Is(err, fixture.want) {
				t.Fatalf("runtime archive admission=%v want%v", err, fixture.want)
			}
			if tx.queries != 1 {
				t.Fatal("runtime-only schema queried notification state")
			}
		})
	}
}

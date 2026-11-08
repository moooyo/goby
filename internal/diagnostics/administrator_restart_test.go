package diagnostics

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
)

func TestAdministratorRestartDiagnosticKeepsOnlyTheClassifiedFact(t *testing.T) {
	var output bytes.Buffer
	handler := NewHandler(nil, slog.NewJSONHandler(&output, nil)).WithAttrs([]slog.Attr{
		slog.String("password", "private-restart-password"),
	})
	record := diagnosticTestRecord("administrator restart rebuilding application generation",
		slog.String("event", "forged-event"), slog.String("token", "private-restart-token"),
		slog.String("database", "postgresql://private-restart-database"))
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	actual := diagnosticJSON(t, output.Bytes())
	if actual["event"] != "server.restarting" || actual["msg"] != "administrator restart rebuilding application generation" || len(actual) != 4 {
		t.Fatalf("restart fact was not classified independently of supplied attributes: %#v", actual)
	}
	if bytes.Contains(output.Bytes(), []byte("private-")) || bytes.Contains(output.Bytes(), []byte("forged-event")) {
		t.Fatal("restart diagnostics disclosed unapproved attributes")
	}
}

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/recovery"
)

type backupPolicyTestManager struct {
	backupTestManager
	view     recovery.BackupPolicyView
	update   func(recovery.BackupPolicyRequest)
	patch    func(recovery.BackupPolicySettingsRequest)
	patchErr error
	calls    int
}

func (m *backupPolicyTestManager) BackupPolicy(context.Context, identity.Principal) (recovery.BackupPolicyView, error) {
	m.calls++
	return m.view, nil
}
func (m *backupPolicyTestManager) UpdateBackupPolicy(_ context.Context, _ identity.Principal, request recovery.BackupPolicyRequest) (recovery.BackupPolicyView, error) {
	m.calls++
	if m.update != nil {
		m.update(request)
	}
	return m.view, nil
}

func (m *backupPolicyTestManager) UpdateBackupPolicySettings(_ context.Context, _ identity.Principal, request recovery.BackupPolicySettingsRequest) (recovery.BackupPolicyView, error) {
	m.calls++
	if m.patch != nil {
		m.patch(request)
	}
	return m.view, m.patchErr
}

const validBackupPolicyJSON = `{"Revision":"9007199254740993","Enabled":true,"Schedule":{"Kind":"daily","DayOfWeek":null,"TimeOfDay":"03:00","Timezone":"Asia/Shanghai"},"KeepCount":7,"IncludeImages":false}`

func TestAdminBackupPolicyPreservesExactRevisionAndClearsSecretBuffers(t *testing.T) {
	for _, suffix := range []string{"", `,"Passphrase":""`, `,"Passphrase":"  exact-backup-secret  "`, `,"Passphrase":"twelve\n\t\u0000\ud83d\ude80"`} {
		var retained []byte
		manager := &backupPolicyTestManager{view: recovery.BackupPolicyView{Revision: "9007199254740994"}}
		manager.update = func(request recovery.BackupPolicyRequest) {
			if request.Revision != "9007199254740993" || request.Schedule.Timezone != "Asia/Shanghai" || request.IncludeImages || request.KeepCount != 7 {
				t.Fatal("policy mutation changed exact scalar input")
			}
			if suffix == "" {
				if request.Passphrase != nil {
					t.Fatal("omitted passphrase was treated as a clear")
				}
			} else {
				if request.Passphrase == nil {
					t.Fatal("explicit passphrase action was lost")
				}
				retained = *request.Passphrase
			}
		}
		body := strings.TrimSuffix(validBackupPolicyJSON, "}") + suffix + "}"
		r := backupTestRequest(body)
		r.Method = http.MethodPut
		response := httptest.NewRecorder()
		(&Server{recovery: manager}).updateAdminBackupPolicy(backupTestResponse{response}, r)
		if response.Code != http.StatusOK || manager.calls != 1 {
			t.Fatalf("valid policy rejected: %d %s", response.Code, response.Body.String())
		}
		if !bytes.Equal(retained, make([]byte, len(retained))) {
			t.Fatal("HTTP handler retained an admitted plaintext passphrase")
		}
		if strings.Contains(response.Body.String(), "\"Passphrase\":") {
			t.Fatal("policy response exposed a secret field")
		}
	}
}

func TestAdminBackupPolicyRejectsMalformedOrAmbiguousObjectsBeforeService(t *testing.T) {
	for _, test := range []struct{ name, body string }{
		{"unknown", strings.TrimSuffix(validBackupPolicyJSON, "}") + `,"Path":"/private/backup"}`},
		{"duplicate", strings.TrimSuffix(validBackupPolicyJSON, "}") + `,"Revision":"1"}`},
		{"escaped_duplicate", strings.TrimSuffix(validBackupPolicyJSON, "}") + `,"\u0052evision":"1"}`},
		{"nested_unknown", strings.Replace(validBackupPolicyJSON, `"Kind":"daily"`, `"Kind":"daily","Private":true`, 1)},
		{"nested_duplicate", strings.Replace(validBackupPolicyJSON, `"Kind":"daily"`, `"Kind":"daily","Kind":"weekly"`, 1)},
		{"null_boolean", strings.Replace(validBackupPolicyJSON, `"Enabled":true`, `"Enabled":null`, 1)},
		{"numeric_revision", strings.Replace(validBackupPolicyJSON, `"9007199254740993"`, `9007199254740993`, 1)},
		{"null_schedule", strings.Replace(validBackupPolicyJSON, `{"Kind":"daily","DayOfWeek":null,"TimeOfDay":"03:00","Timezone":"Asia/Shanghai"}`, `null`, 1)},
		{"null_secret", strings.TrimSuffix(validBackupPolicyJSON, "}") + `,"Passphrase":null}`},
		{"short_secret", strings.TrimSuffix(validBackupPolicyJSON, "}") + `,"Passphrase":"short"}`},
		{"invalid_unicode", strings.TrimSuffix(validBackupPolicyJSON, "}") + `,"Passphrase":"twelve\ud800bytes"}`},
		{"trailing", validBackupPolicyJSON + ` {}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := &backupPolicyTestManager{}
			response := httptest.NewRecorder()
			r := backupTestRequest(test.body)
			r.Method = http.MethodPut
			(&Server{recovery: manager}).updateAdminBackupPolicy(backupTestResponse{response}, r)
			if response.Code != http.StatusBadRequest || manager.calls != 0 {
				t.Fatalf("malformed policy reached service: status=%d calls=%d", response.Code, manager.calls)
			}
			var envelope struct{ Error struct{ Code string } }
			if json.Unmarshal(response.Body.Bytes(), &envelope) != nil || envelope.Error.Code != "invalid_input" {
				t.Fatal("malformed policy lost the native error envelope")
			}
		})
	}
}

const validBackupPolicySettingsJSON = `{"Revision":"9007199254740993","KeepCount":14,"IncludeImages":false}`

func TestAdminBackupPolicyPatchPreservesSettingsAndClearsSecretBuffers(t *testing.T) {
	for _, suffix := range []string{"", `,"Passphrase":""`, `,"Passphrase":"  exact-backup-secret  "`, `,"Passphrase":"twelve\n\t\u0000\ud83d\ude80"`} {
		var retained []byte
		manager := &backupPolicyTestManager{view: recovery.BackupPolicyView{Revision: "9007199254740994", Schedule: recovery.BackupSchedule{Kind: "custom"}}}
		manager.patch = func(request recovery.BackupPolicySettingsRequest) {
			if request.Revision != "9007199254740993" || request.IncludeImages || request.KeepCount != 14 {
				t.Fatal("settings mutation changed exact scalar input")
			}
			if suffix == "" {
				if request.Passphrase != nil {
					t.Fatal("omitted passphrase was treated as a clear")
				}
			} else {
				if request.Passphrase == nil {
					t.Fatal("explicit passphrase action was lost")
				}
				retained = *request.Passphrase
			}
		}
		r := backupTestRequest(strings.TrimSuffix(validBackupPolicySettingsJSON, "}") + suffix + "}")
		r.Method = http.MethodPatch
		response := httptest.NewRecorder()
		(&Server{recovery: manager}).patchAdminBackupPolicy(backupTestResponse{response}, r)
		if response.Code != http.StatusOK || manager.calls != 1 {
			t.Fatalf("valid settings patch rejected: %d %s", response.Code, response.Body.String())
		}
		if !bytes.Equal(retained, make([]byte, len(retained))) {
			t.Fatal("settings handler retained an admitted plaintext passphrase")
		}
		if strings.Contains(response.Body.String(), `"Passphrase":`) {
			t.Fatal("settings response exposed a secret field")
		}
	}
}

func TestAdminBackupPolicyPatchRejectsSchedulingAndAmbiguousInput(t *testing.T) {
	for _, test := range []struct{ name, body string }{
		{"enabled", strings.TrimSuffix(validBackupPolicySettingsJSON, "}") + `,"Enabled":false}`},
		{"schedule", strings.TrimSuffix(validBackupPolicySettingsJSON, "}") + `,"Schedule":{"Kind":"daily"}}`},
		{"unknown", strings.TrimSuffix(validBackupPolicySettingsJSON, "}") + `,"Path":"/private/backup"}`},
		{"duplicate", strings.TrimSuffix(validBackupPolicySettingsJSON, "}") + `,"Revision":"1"}`},
		{"escaped_duplicate", strings.TrimSuffix(validBackupPolicySettingsJSON, "}") + `,"\u004beepCount":7}`},
		{"missing_revision", strings.Replace(validBackupPolicySettingsJSON, `"Revision":"9007199254740993",`, "", 1)},
		{"numeric_revision", strings.Replace(validBackupPolicySettingsJSON, `"9007199254740993"`, `9007199254740993`, 1)},
		{"null_images", strings.Replace(validBackupPolicySettingsJSON, `"IncludeImages":false`, `"IncludeImages":null`, 1)},
		{"null_count", strings.Replace(validBackupPolicySettingsJSON, `"KeepCount":14`, `"KeepCount":null`, 1)},
		{"null_secret", strings.TrimSuffix(validBackupPolicySettingsJSON, "}") + `,"Passphrase":null}`},
		{"short_secret", strings.TrimSuffix(validBackupPolicySettingsJSON, "}") + `,"Passphrase":"short"}`},
		{"invalid_unicode", strings.TrimSuffix(validBackupPolicySettingsJSON, "}") + `,"Passphrase":"twelve\ud800bytes"}`},
		{"trailing", validBackupPolicySettingsJSON + ` {}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := &backupPolicyTestManager{}
			response := httptest.NewRecorder()
			r := backupTestRequest(test.body)
			r.Method = http.MethodPatch
			(&Server{recovery: manager}).patchAdminBackupPolicy(backupTestResponse{response}, r)
			if response.Code != http.StatusBadRequest || manager.calls != 0 {
				t.Fatalf("malformed settings reached service: status=%d calls=%d", response.Code, manager.calls)
			}
			var envelope struct{ Error struct{ Code string } }
			if json.Unmarshal(response.Body.Bytes(), &envelope) != nil || envelope.Error.Code != "invalid_input" {
				t.Fatal("malformed settings lost the native error envelope")
			}
		})
	}
}

func TestAdminBackupPolicyPatchPreservesCredentialValidationAndConflict(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{
		{"active_rules", &recovery.BackupPolicyValidationError{Fields: map[string]string{"Passphrase": "Remove all automatic backup triggers on Tasks before clearing the saved passphrase."}}, http.StatusBadRequest},
		{"stale_revision", recovery.ErrConflict, http.StatusConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			var retained []byte
			manager := &backupPolicyTestManager{patchErr: test.err, patch: func(request recovery.BackupPolicySettingsRequest) { retained = *request.Passphrase }}
			r := backupTestRequest(strings.TrimSuffix(validBackupPolicySettingsJSON, "}") + `,"Passphrase":"private-backup-secret"}`)
			r.Method = http.MethodPatch
			response := httptest.NewRecorder()
			(&Server{recovery: manager}).patchAdminBackupPolicy(backupTestResponse{response}, r)
			if response.Code != test.status || manager.calls != 1 {
				t.Fatalf("settings rejection lost its status: %d %s", response.Code, response.Body.String())
			}
			if !bytes.Equal(retained, make([]byte, len(retained))) || strings.Contains(response.Body.String(), "private-backup-secret") {
				t.Fatal("rejected settings retained or exposed their passphrase")
			}
			if test.name == "active_rules" {
				var envelope struct {
					Error struct{ Fields map[string]string }
				}
				if json.Unmarshal(response.Body.Bytes(), &envelope) != nil || envelope.Error.Fields["Passphrase"] == "" {
					t.Fatal("active-trigger rejection lost its field-level guidance")
				}
			}
		})
	}
}

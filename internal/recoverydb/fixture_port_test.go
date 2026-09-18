package recoverydb

import (
	"errors"
	"strconv"
	"testing"
)

const recoveryFixtureHost = "127.0.0.1"
const recoveryFixtureDefaultPort uint16 = 15432

// Parsing stays independent of networking and ambient environment mutation.
// An explicitly empty variable is invalid; only an absent variable defaults.
func parseRecoveryFixturePort(raw string, present bool) (uint16, error) {
	if !present {
		return recoveryFixtureDefaultPort, nil
	}
	invalid := errors.New("GOBY_TEST_RECOVERY_PORT must be a canonical decimal port between 1 and 65535")
	if len(raw) == 0 || len(raw) > 5 {
		return 0, invalid
	}
	for _, character := range raw {
		if character < '0' || character > '9' {
			return 0, invalid
		}
	}
	value, err := strconv.ParseUint(raw, 10, 16)
	if err != nil || value == 0 || strconv.FormatUint(value, 10) != raw {
		return 0, invalid
	}
	return uint16(value), nil
}

func TestRecoveryFixturePortEnvironmentParsing(t *testing.T) {
	for _, test := range []struct {
		name, raw string
		present   bool
		want      uint16
	}{
		{"absent preserves original cluster", "", false, 15432},
		{"explicit original cluster", "15432", true, 15432},
		{"explicit independently owned cluster", "54435", true, 54435},
		{"minimum TCP port", "1", true, 1},
		{"maximum TCP port", "65535", true, 65535},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual, err := parseRecoveryFixturePort(test.raw, test.present)
			if err != nil || actual != test.want {
				t.Fatalf("parsed fixture port = %d, want %d: %v", actual, test.want, err)
			}
		})
	}
	for _, raw := range []string{
		"", "0", "65536", "99999999999999999999", "015432", "00001", "+54435", "-1",
		" 54435", "54435 ", "54435\n", "54435\x00", "54.435", "5e4", "５４４３５",
		"127.0.0.1:54435", "[::1]:54435", "postgresql://127.0.0.1:54435", "54435,15432", "54435/other",
	} {
		t.Run("reject_"+strconv.Quote(raw), func(t *testing.T) {
			if port, err := parseRecoveryFixturePort(raw, true); err == nil || port != 0 {
				t.Fatalf("invalid explicit port was accepted: port=%d error=%v", port, err)
			}
		})
	}
}

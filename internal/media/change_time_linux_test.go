package media

import (
	"os"
	"syscall"
	"testing"
	"time"
)

func TestFileChangeTime(t *testing.T) {
	var nilStat *syscall.Stat_t
	for _, test := range []struct {
		name string
		info os.FileInfo
		want int64
	}{
		{"normal", changeTimeFileInfo{sys: &syscall.Stat_t{Ctim: syscall.Timespec{Sec: 1234, Nsec: 567890123}}}, 1_234_567_890_123},
		{"negative", changeTimeFileInfo{sys: &syscall.Stat_t{Ctim: syscall.Timespec{Sec: -1, Nsec: 999999999}}}, -1},
		{"zero", changeTimeFileInfo{sys: &syscall.Stat_t{}}, 0},
		{"nil info", nil, 0},
		{"nil sys", changeTimeFileInfo{}, 0},
		{"wrong sys type", changeTimeFileInfo{sys: struct{}{}}, 0},
		{"nonpointer stat", changeTimeFileInfo{sys: syscall.Stat_t{}}, 0},
		{"nil stat", changeTimeFileInfo{sys: nilStat}, 0},
		{"negative nanoseconds", changeTimeFileInfo{sys: &syscall.Stat_t{Ctim: syscall.Timespec{Sec: 1, Nsec: -1}}}, 0},
		{"excess nanoseconds", changeTimeFileInfo{sys: &syscall.Stat_t{Ctim: syscall.Timespec{Sec: 1, Nsec: 1000000000}}}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := FileChangeTime(test.info); got != test.want {
				t.Fatalf("FileChangeTime() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestChangeTimeNanoseconds(t *testing.T) {
	for _, test := range []struct {
		name        string
		seconds     int64
		nanoseconds int64
		want        int64
	}{
		{"zero", 0, 0, 0},
		{"nanoseconds only", 0, 123, 123},
		{"normal", 1234, 567890123, 1_234_567_890_123},
		{"negative second", -1, 0, -1_000_000_000},
		{"negative nanosecond", -1, 999999999, -1},
		{"negative nsec invalid", 1, -1, 0},
		{"one second nsec invalid", 1, 1000000000, 0},
		{"maximum", 9223372036, 854775807, 9223372036854775807},
		{"below maximum", 9223372036, 854775806, 9223372036854775806},
		{"positive addition overflow", 9223372036, 854775808, 0},
		{"positive seconds overflow", 9223372037, 0, 0},
		{"extreme positive seconds", 9223372036854775807, 999999999, 0},
		{"minimum", -9223372037, 145224192, -9223372036854775808},
		{"above minimum", -9223372037, 145224193, -9223372036854775807},
		{"minimum second largest nsec", -9223372037, 999999999, -9223372036000000001},
		{"negative addition overflow", -9223372037, 145224191, 0},
		{"negative seconds overflow", -9223372038, 999999999, 0},
		{"extreme negative seconds", -9223372036854775808, 999999999, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := changeTimeNanoseconds(test.seconds, test.nanoseconds); got != test.want {
				t.Fatalf("changeTimeNanoseconds(%d, %d) = %d, want %d", test.seconds, test.nanoseconds, got, test.want)
			}
		})
	}
}

type changeTimeFileInfo struct {
	sys any
}

func (changeTimeFileInfo) Name() string       { return "fixture" }
func (changeTimeFileInfo) Size() int64        { return 0 }
func (changeTimeFileInfo) Mode() os.FileMode  { return 0 }
func (changeTimeFileInfo) ModTime() time.Time { return time.Time{} }
func (changeTimeFileInfo) IsDir() bool        { return false }
func (info changeTimeFileInfo) Sys() any      { return info.sys }

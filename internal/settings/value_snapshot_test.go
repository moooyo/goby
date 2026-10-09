package settings

import (
	"strconv"
	"sync"
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

func valueSnapshotTestPublication(revision int64) Snapshot {
	name := strconv.FormatInt(revision, 10)
	threads := int(revision)
	return Snapshot{Revision: revision, Effective: Values{ServerName: name, MaxBitrate: revision},
		Overrides: Overrides{ServerName: &name}, Encoding: Encoding{TranscodingMaxWidth: int(revision)},
		Management: Management{Subtitles: SubtitleOptions{DownloadLanguages: []string{"en", "fr"}}},
		Sorting:    Sorting{SortRemoveWords: []string{"the", "a"}},
		Runtime: RuntimeSnapshot{DesiredNetwork: NetworkValues{BindHost: name, HttpPort: int(revision)},
			Hardware:  HardwareSelection{Decode: "software", Encode: "software", DeviceID: name},
			Execution: transcode.DefaultExecutionOptions(threads), Overrides: RuntimeOverrides{Threads: &threads}}}
}

func TestManagedSettingsValueSnapshotKeepsOneRevisionDuringPublication(t *testing.T) {
	store := &Store{}
	store.publish(valueSnapshotTestPublication(1))
	var readers sync.WaitGroup
	finished := make(chan struct{})
	for range 8 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				value := store.ValueSnapshot()
				name := strconv.FormatInt(value.Revision, 10)
				if value.Effective.ServerName != name || value.Effective.MaxBitrate != value.Revision ||
					value.Encoding.TranscodingMaxWidth != int(value.Revision) || value.DesiredNetwork.BindHost != name ||
					value.DesiredNetwork.HttpPort != int(value.Revision) || value.Hardware.DeviceID != name || value.Execution.Threads != int(value.Revision) {
					t.Error("value projection mixed independently published revisions")
					return
				}
				select {
				case <-finished:
					return
				default:
				}
			}
		}()
	}
	for revision := int64(2); revision <= 256; revision++ {
		store.publish(valueSnapshotTestPublication(revision))
	}
	close(finished)
	readers.Wait()
}

func TestManagedSettingsValueSnapshotDoesNotWeakenFullSnapshotOwnership(t *testing.T) {
	store := &Store{}
	input := valueSnapshotTestPublication(7)
	store.publish(input)
	want := store.ValueSnapshot()
	*input.Overrides.ServerName = "changed input"
	*input.Runtime.Overrides.Threads = 30
	input.Management.Subtitles.DownloadLanguages[0] = "de"
	input.Sorting.SortRemoveWords[0] = "changed input"
	full := store.Snapshot()
	if *full.Overrides.ServerName != "7" || *full.Runtime.Overrides.Threads != 7 ||
		full.Management.Subtitles.DownloadLanguages[0] != "en" || full.Sorting.SortRemoveWords[0] != "the" {
		t.Fatal("publication retained mutable input aliases")
	}
	*full.Overrides.ServerName = "changed result"
	*full.Runtime.Overrides.Threads = 40
	full.Management.Subtitles.DownloadLanguages[0] = "es"
	full.Sorting.SortRemoveWords[0] = "changed result"
	value := store.ValueSnapshot()
	value.Effective.ServerName = "changed value"
	value.Hardware.DeviceID = "changed device"
	value.Execution.Threads = 50
	if current := store.ValueSnapshot(); current != want {
		t.Fatal("a returned snapshot changed the published value projection")
	}
	fresh := store.Snapshot()
	if *fresh.Overrides.ServerName != "7" || *fresh.Runtime.Overrides.Threads != 7 ||
		fresh.Management.Subtitles.DownloadLanguages[0] != "en" || fresh.Sorting.SortRemoveWords[0] != "the" {
		t.Fatal("the value accessor weakened full snapshot defensive copies")
	}
}

func TestManagedSettingsValueSnapshotAvoidsUnusedCopyAllocations(t *testing.T) {
	store := &Store{}
	store.publish(valueSnapshotTestPublication(7))
	var value ValueSnapshot
	valueAllocations := testing.AllocsPerRun(100, func() { value = store.ValueSnapshot() })
	var full Snapshot
	fullAllocations := testing.AllocsPerRun(100, func() { full = store.Snapshot() })
	if value.Revision != full.Revision || valueAllocations >= fullAllocations {
		t.Fatalf("value projection allocations = %g; full snapshot allocations = %g", valueAllocations, fullAllocations)
	}
	t.Logf("value projection allocations = %g; full snapshot allocations = %g", valueAllocations, fullAllocations)
}

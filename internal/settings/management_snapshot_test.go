package settings

import (
	"sync"
	"testing"
)

func alternateManagementProjectionOptions() Management {
	return Management{
		Metadata: MetadataOptions{EnableInternetProviders: true, PreferredMetadataLanguage: "fr", MetadataCountryCode: "FR"},
		Subtitles: SubtitleOptions{DownloadLanguages: []string{"fr", "de"},
			DownloadMovieSubtitles: false, DownloadEpisodeSubtitles: false},
		Tasks: TaskOptions{MaxConcurrent: 4, CacheRetentionDays: 60, CacheMaxEntries: 20000},
	}
}

func TestManagementSnapshotOwnsLanguagesAndKeepsCapturedOptions(t *testing.T) {
	store := &Store{}
	input := alternateManagementProjectionOptions()
	store.publish(Snapshot{Revision: 1, Management: input})
	input.Subtitles.DownloadLanguages[0] = "changed input"
	want := alternateManagementProjectionOptions()
	if !equalManagement(store.ManagementSnapshot(), want) {
		t.Fatal("published management options retained the input language slice")
	}
	returned := store.ManagementSnapshot()
	returned.Subtitles.DownloadLanguages[0] = "changed projection"
	full := store.Snapshot()
	if !equalManagement(full.Management, want) {
		t.Fatal("the management projection exposed the published language slice")
	}
	full.Management.Subtitles.DownloadLanguages[0] = "changed full snapshot"
	captured := store.ManagementSnapshot()
	if !equalManagement(captured, want) || store.MetadataValues() != want.Metadata ||
		store.TaskMaxConcurrent() != want.Tasks.MaxConcurrent || !store.InternetProvidersEnabled() {
		t.Fatal("a full snapshot changed the independently captured management values")
	}

	next := DefaultManagement()
	store.publish(Snapshot{Revision: 2, Management: next})
	if !equalManagement(captured, want) || !equalManagement(store.ManagementSnapshot(), next) ||
		store.MetadataValues() != next.Metadata || store.TaskMaxConcurrent() != next.Tasks.MaxConcurrent || store.InternetProvidersEnabled() {
		t.Fatal("a later publication changed captured options or was absent from a fresh projection")
	}
	next.Subtitles.DownloadLanguages = []string{}
	store.publish(Snapshot{Revision: 3, Management: next})
	if languages := store.ManagementSnapshot().Subtitles.DownloadLanguages; languages == nil || len(languages) != 0 {
		t.Fatal("an explicit empty subtitle language selection was lost")
	}
}

func TestManagementProjectionsKeepOneRevisionDuringPublication(t *testing.T) {
	first, second := DefaultManagement(), alternateManagementProjectionOptions()
	store := &Store{}
	store.publish(Snapshot{Revision: 1, Management: first})
	var readers sync.WaitGroup
	started := make(chan struct{}, 8)
	finished := make(chan struct{})
	for range 8 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			started <- struct{}{}
			for {
				management := store.ManagementSnapshot()
				if !equalManagement(management, first) && !equalManagement(management, second) {
					t.Error("management capture mixed independently published revisions")
					return
				}
				metadata := store.MetadataValues()
				if metadata != first.Metadata && metadata != second.Metadata {
					t.Error("metadata projection mixed independently published revisions")
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
	for range 8 {
		<-started
	}
	for revision := int64(2); revision <= 256; revision++ {
		management := first
		if revision%2 == 0 {
			management = second
		}
		store.publish(Snapshot{Revision: revision, Management: management})
	}
	close(finished)
	readers.Wait()
}

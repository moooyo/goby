package library

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
)

type auxiliaryCapacityProber struct {
	libraryFixtureProber
	maximum atomic.Int64
}

func (prober *auxiliaryCapacityProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	owners := int64(originalMediaReadOwners.Stats().RegisteredOwners)
	for current := prober.maximum.Load(); owners > current; current = prober.maximum.Load() {
		if prober.maximum.CompareAndSwap(current, owners) {
			break
		}
	}
	return prober.libraryFixtureProber.ProbeFile(ctx, file)
}

func TestAuxiliaryGroupRetiresEachInputBeforeNextCandidate(t *testing.T) {
	for _, role := range []string{"theme", "extra"} {
		for _, consumers := range []int{0, 24} {
			t.Run(fmt.Sprintf("%s/retained_%d", role, consumers), func(t *testing.T) {
				prober := &auxiliaryCapacityProber{}
				ctx, pool, store, root, _ := libraryIntegrationStoreWithTimeout(t, prober, 3*time.Minute)
				libraryIntegrationFile(t, root, "movies/Film/Main.mp4", "video:main")
				library := libraryIntegrationCreate(t, ctx, store, "Bounded auxiliary group", "movies", filepath.Join(root, "movies"))
				baseline := originalMediaReadOwners.Stats().RegisteredOwners
				held := make([]*primaryio.Owner, 0, consumers)
				defer func() {
					for _, owner := range held {
						if err := owner.Complete(); err != nil {
							t.Errorf("retire competing consumer: %v", err)
						}
					}
				}()
				for range consumers {
					owner, err := originalMediaReadOwners.Register(ctx)
					if err != nil {
						t.Fatal(err)
					}
					held = append(held, owner)
				}
				directory, extension, contents := "theme-music", "mp3", "audio:theme"
				if role == "extra" {
					directory, extension, contents = "featurettes", "mp4", "video:extra"
				}
				previous := 0
				for _, count := range []int{32, 33, 64, 65, 256} {
					for index := previous; index < count; index++ {
						libraryIntegrationFile(t, root, fmt.Sprintf("movies/Film/%s/%03d.%s", directory, index, extension), contents)
					}
					job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
					if job.Error != "" || job.Scanned != count+1 {
						t.Fatalf("legal %d-resource group did not finish completely: %+v", count, job)
					}
					resources := themeScanTestResources(t, ctx, pool, library.ID)
					if role == "extra" {
						resources = extraScanTestResources(t, ctx, pool, library.ID)
					}
					if len(resources) != count {
						t.Fatalf("published %d resources for a complete %d-resource group", len(resources), count)
					}
					for _, resource := range resources {
						if !resource.active {
							t.Fatalf("legal group retained an inactive member: %+v", resource)
						}
					}
					if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != baseline+consumers {
						t.Fatalf("completed group retained source owners: got=%d want=%d", owners, baseline+consumers)
					}
					previous = count
				}
				if maximum := prober.maximum.Load(); maximum > int64(baseline+consumers+8) {
					t.Fatalf("probe owners grew with group size: maximum=%d competing=%d", maximum, consumers)
				}
			})
		}
	}
}

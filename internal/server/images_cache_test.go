package server

import (
	"fmt"
	"sync"
	"testing"

	"github.com/moooyo/goby/internal/artwork"
)

func TestImageCacheBoundsCapacityAndRetainsRecentlyUsedEntries(t *testing.T) {
	cache := newImageCache()
	for index := range imageCacheEntries {
		cache.put(cachedImage{key: fmt.Sprint(index), data: []byte{byte(index)}})
	}
	if _, found := cache.get("0"); !found {
		t.Fatal("initial cache entry is missing")
	}
	cache.put(cachedImage{key: "new", data: []byte{1}})
	if _, found := cache.get("0"); !found {
		t.Error("recently accessed entry was evicted")
	}
	if _, found := cache.get("1"); found {
		t.Error("least recently used entry survived the entry limit")
	}
	cache.put(cachedImage{key: "too-large", data: make([]byte, 1, (8<<20)+1)})
	if _, found := cache.get("too-large"); found {
		t.Error("large backing allocation bypassed the per-entry limit")
	}
	cache = newImageCache()
	for index := range 9 {
		cache.put(cachedImage{key: fmt.Sprint(index), data: make([]byte, 1, 8<<20)})
	}
	if cache.bytes != imageCacheBytes || len(cache.entries) != 8 {
		t.Fatalf("backing allocations are not bounded: bytes=%d, entries=%d", cache.bytes, len(cache.entries))
	}
	if _, found := cache.get("0"); found {
		t.Error("oldest backing allocation was not evicted")
	}
	cache.put(cachedImage{key: "8", data: []byte{3}})
	if cache.bytes != 7*(8<<20)+1 {
		t.Error("replacement did not release the prior entry's accounted capacity")
	}
}

func TestImageCacheSupportsConcurrentReadersAndWriters(t *testing.T) {
	cache := newImageCache()
	var tasks sync.WaitGroup
	for worker := range 8 {
		tasks.Go(func() {
			for iteration := range 400 {
				key := fmt.Sprint((iteration + worker) % 300)
				cache.put(cachedImage{key: key, data: []byte{1, 2, 3}})
				if value, found := cache.get(key); found && (value.key != key || len(value.data) != 3) {
					t.Error("concurrent lookup returned a different entry")
				}
			}
		})
	}
	tasks.Wait()
	if cache.bytes > imageCacheBytes || len(cache.entries) > imageCacheEntries {
		t.Error("concurrent insertion exceeded the cache budget")
	}
}

func TestImageValidatorsDistinguishRequestVariants(t *testing.T) {
	const tag = "source-content-tag"
	plain := imageETag(tag, artwork.Options{})
	width := imageETag(tag, artwork.Options{Width: 64})
	maximum := imageETag(tag, artwork.Options{MaxWidth: 64})
	if plain != `"source-content-tag"` || plain == width || width == maximum {
		t.Fatal("image validators do not distinguish request variants")
	}
	for _, header := range []string{plain, "W/" + plain, `"other", W/` + plain, "*"} {
		if !matchesImageETag(header, plain) {
			t.Errorf("valid conditional header did not match: %s", header)
		}
	}
	for _, header := range []string{"", tag, width, `"other"`, "w/" + plain} {
		if matchesImageETag(header, plain) {
			t.Errorf("unrelated or invalid validator matched: %s", header)
		}
	}
}

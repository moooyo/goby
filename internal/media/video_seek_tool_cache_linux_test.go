package media

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func videoSeekToolCacheQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func videoSeekToolCacheHelper(t *testing.T) (string, string, string, string) {
	t.Helper()
	directory := t.TempDir()
	tool, mode, counter, ready := filepath.Join(directory, "tool"), filepath.Join(directory, "mode"), filepath.Join(directory, "counter"), filepath.Join(directory, "ready")
	program := "#!/bin/sh\nset -eu\n" +
		"test \"${1-}\" = -version\n" +
		"if IFS= read -r unexpected <&3; then exit 91; fi\n" +
		"n=$(cat " + videoSeekToolCacheQuote(counter) + ")\nprintf '%s\\n' \"$((n + 1))\" > " + videoSeekToolCacheQuote(counter) + "\n" +
		"mode=$(cat " + videoSeekToolCacheQuote(mode) + ")\n" +
		"case \"$mode\" in fail) exit 7;; wait) : > " + videoSeekToolCacheQuote(ready) + "; sleep 30 & wait;; rewrite) printf '# changed during version\\n' >> \"$0\";; esac\n" +
		"printf 'controlled version %s loader %s\\n' \"$mode\" \"${LD_LIBRARY_PATH-}\"\n"
	for path, data := range map[string]string{tool: program, mode: "first\n", counter: "0\n"} {
		if err := os.WriteFile(path, []byte(data), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return tool, mode, counter, ready
}

func videoSeekToolCacheMode(t *testing.T, path, mode string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(mode+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
}

func TestVideoSeekToolIdentityCacheFreshVersionEnvironmentAndEOF(t *testing.T) {
	t.Setenv("LD_LIBRARY_PATH", "")
	tool, mode, counter, _ := videoSeekToolCacheHelper(t)
	cache := &videoSeekToolHashCache{bufferBytes: 16 << 10}
	identify := func(cache *videoSeekToolHashCache) string {
		t.Helper()
		path, value, err := videoSeekToolIdentity(context.Background(), tool, cache)
		if err != nil || path != tool {
			t.Fatalf("tool identification failed: %s %v", path, err)
		}
		return value
	}
	first := identify(cache)
	if videoSeekToolTestReady(cache) != 1 {
		t.Fatal("successful identity did not publish its prefix")
	}
	if second := identify(cache); second != first {
		t.Fatal("unchanged warm identity differs from the full read")
	}
	count, err := os.ReadFile(counter)
	if err != nil || strings.TrimSpace(string(count)) != "2" {
		t.Fatalf("warm identity skipped the fresh version process: %s %v", count, err)
	}
	videoSeekToolCacheMode(t, mode, "second")
	secondVersion := identify(cache)
	if secondVersion == first || identify(nil) != secondVersion {
		t.Fatal("external version changes were hidden by cached bytes")
	}
	t.Setenv("LD_LIBRARY_PATH", t.TempDir())
	changedEnvironment := identify(cache)
	if changedEnvironment == secondVersion || identify(nil) != changedEnvironment {
		t.Fatal("current loader environment was not included")
	}

	original, err := os.ReadFile(tool)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(tool)
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(filepath.Dir(tool), "replacement")
	if err := os.WriteFile(replacement, original, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(replacement, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, tool); err != nil {
		t.Fatal(err)
	}
	replaced := identify(cache)
	if replaced == changedEnvironment || identify(nil) != replaced {
		t.Fatal("same-byte pathname replacement retained the old filesystem identity")
	}

	videoSeekToolCacheMode(t, mode, "fail")
	if _, _, err := videoSeekToolIdentity(context.Background(), tool, cache); err == nil {
		t.Fatal("a cached prefix hid a failed fresh version process")
	}
	if videoSeekToolTestReady(cache) != 0 {
		t.Fatal("failed version published cached bytes")
	}
	videoSeekToolCacheMode(t, mode, "third")
	_ = identify(cache)
	videoSeekToolCacheMode(t, mode, "rewrite")
	if _, _, err := videoSeekToolIdentity(context.Background(), tool, cache); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("post-version mutation was not rejected: %v", err)
	}
	if videoSeekToolTestReady(cache) != 0 {
		t.Fatal("post-stat failure published a prefix")
	}
}

func TestVideoSeekToolIdentityCacheCancellationDuringFreshVersion(t *testing.T) {
	tool, mode, _, ready := videoSeekToolCacheHelper(t)
	cache := &videoSeekToolHashCache{bufferBytes: 16 << 10}
	if _, _, err := videoSeekToolIdentity(context.Background(), tool, cache); err != nil {
		t.Fatal(err)
	}
	videoSeekToolCacheMode(t, mode, "wait")
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		defer close(finished)
		_, _, err := videoSeekToolIdentity(ctx, tool, cache)
		result <- err
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("canceled identity retained its version worker")
		}
	})
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("fresh version did not wait: %v", err)
		case <-deadline:
			t.Fatal("fresh version did not start")
		case <-ticker.C:
		}
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("warm version cancellation became a cached success: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("version cancellation did not return")
	}
	if videoSeekToolTestReady(cache) != 0 {
		t.Fatal("canceled version published a prefix")
	}
}

func TestVideoSeekToolIdentityCacheDoesNotPublishCanceledCaller(t *testing.T) {
	tool, _, counter, _ := videoSeekToolCacheHelper(t)
	cache := &videoSeekToolHashCache{bufferBytes: 16 << 10}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := videoSeekToolIdentity(ctx, tool, cache); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled caller: %v", err)
	}
	count, err := os.ReadFile(counter)
	if err != nil {
		t.Fatal(err)
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(count)))
	if err != nil || value != 0 || videoSeekToolTestReady(cache) != 0 {
		t.Fatalf("canceled caller ran or published: count=%q err=%v", count, err)
	}
}

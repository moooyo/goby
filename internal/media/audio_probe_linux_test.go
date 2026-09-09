package media

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAudioProbeUsesIsolatedDescriptorEnvironment(t *testing.T) {
	directory := t.TempDir()
	helper := filepath.Join(directory, "ffprobe-helper")
	metadata := `{"format":{"format_name":"wav","duration":"9"},"streams":[{"index":0,"codec_name":"pcm_s16le","codec_type":"audio","sample_rate":"48000","channels":1,"time_base":"1/48000"}]}`
	scan := `{"packets_and_frames":[{"type":"packet","stream_index":0,"pts":0,"duration":4,"pos":0},{"type":"frame","stream_index":0,"pts":0,"nb_samples":4,"pkt_pos":0}]}`
	program := "#!/bin/sh\nset -eu\n" +
		"if [ -n \"${GOBY_DATABASE_URL-}${FFREPORT-}${LD_PRELOAD-}\" ]; then exit 91; fi\n" +
		"case \" $* \" in *' -show_format '*) printf '%s\\n' '" + metadata + "';; *) printf '%s\\n' '" + scan + "';; esac\n"
	if err := os.WriteFile(helper, []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "audio.wav")
	if err := os.WriteFile(path, make([]byte, 8), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOBY_DATABASE_URL", "must-not-reach-parser")
	t.Setenv("FFREPORT", "file=must-not-write-report")
	t.Setenv("LD_PRELOAD", "/must-not-load-untrusted-library")
	info, err := (Prober{FFprobePath: helper, Timeout: 5 * time.Second}).Probe(context.Background(), path)
	if err != nil || !info.AudioDurationExact || info.DurationTicks != 834 || info.Streams[0].AudioTiming.SampleCount != 4 {
		t.Fatalf("isolated descriptor probe did not retain accurate audio: %+v, %v", info, err)
	}
}

func TestMediaDescriptorCancellationRetiresTheWholeProcessGroup(t *testing.T) {
	directory := t.TempDir()
	helper := filepath.Join(directory, "process-group-helper")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nset -eu\nsleep 30 &\nchild=$!\nprintf '%s\\n' \"$child\" >&4\nwait \"$child\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	input, err := os.CreateTemp(directory, "borrowed-input-")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err := input.Seek(2, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	readyRead, readyWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readyRead.Close()
	defer readyWrite.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := runLimitedFiles(ctx, 10*time.Second, 1024, helper, []*os.File{input, readyWrite})
		result <- err
	}()
	type childResult struct {
		pid string
		err error
	}
	childReady := make(chan childResult, 1)
	go func() {
		pid, err := bufio.NewReader(readyRead).ReadString('\n')
		childReady <- childResult{pid: strings.TrimSpace(pid), err: err}
	}()
	var pid int
	select {
	case child := <-childReady:
		pid, err = strconv.Atoi(child.pid)
		if child.err != nil || err != nil || pid <= 0 {
			t.Fatalf("process group did not report its child: %+v", child)
		}
	case err := <-result:
		t.Fatalf("process exited before spawning its controlled descendant: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("process did not spawn its controlled descendant")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("descriptor cancellation returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled descriptor process did not return")
	}
	deadline, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		closing := strings.LastIndexByte(string(stat), ')')
		if err == nil && closing >= 0 && len(stat) > closing+2 && stat[closing+2] == 'Z' {
			break
		}
		select {
		case <-tick.C:
		case <-deadline.Done():
			t.Fatal("cancelled media process left a running descendant")
		}
	}
	position, err := input.Seek(0, io.SeekCurrent)
	if err != nil || position != 2 {
		t.Errorf("cancellation closed or sought the caller's descriptor: position %d, error %v", position, err)
	}
}

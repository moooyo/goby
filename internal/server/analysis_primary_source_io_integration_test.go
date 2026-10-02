//go:build linux && primary_io_measure

package server

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
	"golang.org/x/sys/unix"
)

const analysisPrimarySourceIOReadRate = "1"

// The existing fixture seals this forwarding tool normally. Only its actual
// raw-media invocation is paced; version and capability output remain official.
func analysisPrimarySourceIOPacedTool(t *testing.T) os.FileInfo {
	t.Helper()
	original, err := exec.LookPath(os.Getenv("GOBY_INTRO_SKIPPER_FFMPEG"))
	if err == nil {
		original, err = filepath.Abs(original)
	}
	if err == nil {
		original, err = filepath.EvalSymlinks(original)
	}
	if err != nil {
		t.Fatal("resolve the official intro FFmpeg dependency")
	}
	info, err := os.Stat(original)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatal("the official intro FFmpeg dependency must be a regular executable")
	}
	wrapper := filepath.Join(t.TempDir(), "paced-intro-ffmpeg")
	// Real-time input pacing keeps this forty-second source live for cancellation
	// while allowing body reads and CPU ticks to advance after initial probing.
	script := "#!/bin/sh\ncase \" $* \" in\n*\" -fp_format raw \"*) exec " + audioHTTPShellQuote(original) + " -readrate " + analysisPrimarySourceIOReadRate + " \"$@\";;\n*) exec " + audioHTTPShellQuote(original) + " \"$@\";;\nesac\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal("write the owned forwarding tool before fixture admission")
	}
	t.Setenv("GOBY_INTRO_SKIPPER_FFMPEG", wrapper)
	return info
}

// Thread-local children inventories avoid an unbounded machine-wide process
// scan. Every candidate must still prove the direct parent and stable identity.
func analysisPrimarySourceIOChildren() (map[int]struct{}, error) {
	threads, err := os.ReadDir("/proc/self/task")
	if err != nil {
		return nil, err
	}
	if len(threads) > 4096 {
		return nil, errors.New("analysis process observation exceeded its thread bound")
	}
	children := make(map[int]struct{})
	for _, thread := range threads {
		data, err := os.ReadFile(filepath.Join("/proc/self/task", thread.Name(), "children"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if len(data) > 64<<10 {
			return nil, errors.New("analysis process observation exceeded its child inventory bound")
		}
		for _, value := range strings.Fields(string(data)) {
			pid, err := strconv.Atoi(value)
			if err != nil || pid <= 1 {
				return nil, errors.New("analysis child identity is invalid")
			}
			children[pid] = struct{}{}
			if len(children) > 4096 {
				return nil, errors.New("analysis process observation exceeded its child bound")
			}
		}
	}
	return children, nil
}

func analysisPrimarySourceIOParent(pid int) (int, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return 0, errors.New("analysis process parent observation is malformed")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return 0, errors.New("analysis process parent observation is incomplete")
	}
	return strconv.Atoi(fields[1])
}

func analysisPrimarySourceIOHasArgumentPair(data []byte, option, value string) bool {
	args := strings.Split(string(data), "\x00")
	for index := 0; index+1 < len(args); index++ {
		if args[index] == option && args[index+1] == value {
			return true
		}
	}
	return false
}

func analysisPrimarySourceIOObserveDecoder(t *testing.T, f introSkipperServerFixture, official os.FileInfo, sources []os.FileInfo) (playbackStopAliasEncoder, int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		children, err := analysisPrimarySourceIOChildren()
		if err != nil {
			t.Fatal("read the bounded direct-child inventory for the actual analysis executor")
		}
		for pid := range children {
			root := filepath.Join("/proc", strconv.Itoa(pid))
			executable, err := os.Stat(filepath.Join(root, "exe"))
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
				continue
			}
			if err != nil {
				t.Fatal("inspect the actual analysis child executable")
			}
			if !os.SameFile(official, executable) {
				continue
			}
			args, err := os.ReadFile(filepath.Join(root, "cmdline"))
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
				continue
			}
			if err != nil || len(args) > 64<<10 {
				t.Fatal("inspect the bounded actual analysis invocation")
			}
			if !analysisPrimarySourceIOHasArgumentPair(args, "-fp_format", "raw") || !analysisPrimarySourceIOHasArgumentPair(args, "-readrate", analysisPrimarySourceIOReadRate) {
				continue
			}
			parent, parentErr := analysisPrimarySourceIOParent(pid)
			state, start, statErr := hlsColdPID(pid)
			group, groupErr := syscall.Getpgid(pid)
			source, sourceErr := os.Stat(filepath.Join(root, "fd", "3"))
			if parentErr != nil || statErr != nil || groupErr != nil || sourceErr != nil ||
				parent != os.Getpid() || state == 'Z' || start == 0 || group != pid {
				t.Fatal("the actual analysis decoder lost its direct parent, process group or source descriptor identity")
			}
			for index, expected := range sources {
				if !os.SameFile(expected, source) {
					continue
				}
				first, err := hlsWallPauseReadProcess(pid, start)
				if err != nil {
					t.Fatal("read the exact live analysis decoder counters")
				}
				pidfd, err := unix.PidfdOpen(pid, 0)
				if err != nil {
					t.Fatal("independently pin the actual analysis decoder before cancellation")
				}
				t.Cleanup(func() {
					if err := unix.Close(pidfd); err != nil {
						t.Errorf("close the independently pinned analysis decoder: %v", err)
					}
				})
				_, confirmed, err := hlsColdPID(pid)
				if err != nil || confirmed != start {
					t.Fatal("the pinned analysis decoder changed identity before observation")
				}
				identity, ok := expected.Sys().(*syscall.Stat_t)
				if !ok {
					t.Fatal("the exact admitted Analysis source device and inode are unavailable")
				}
				t.Logf("analysis_primary_source_io stage=decoder_identified child_identified=true pid=%d ppid=%d pgid=%d start_tick=%d pidfd=%d source_index=%d source_device=%d source_inode=%d exact_fd3=true readrate=%s first_cpu_user_ticks=%d first_cpu_system_ticks=%d first_read_chars=%d first_write_chars=%d",
					pid, parent, group, start, pidfd, index, uint64(identity.Dev), identity.Ino, analysisPrimarySourceIOReadRate,
					first.UserTicks, first.SystemTicks, first.ReadChars, first.WriteChars)
				return playbackStopAliasEncoder{pid: pid, startTick: start, pidfd: pidfd, first: first}, index
			}
			t.Fatal("the actual raw decoder inherited a descriptor outside its admitted cohort")
		}
		select {
		case err := <-f.executor.completed:
			t.Fatalf("the actual executor completed before a live decoder observation: %v", err)
		case <-f.ctx.Done():
			t.Fatal(f.ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	t.Fatal("the production analysis executor did not expose an actual official raw decoder")
	return playbackStopAliasEncoder{}, 0
}

// These are bounded scalar observations, not atomic peaks. They distinguish a
// missing source phase from a live decoder whose pre-read counters did not grow.
type analysisPrimarySourceIOLiveObservations struct {
	samples                                      int
	cpuGrowth, characterIOGrowth, chargeObserved bool
	last, maximum                                hlsWallPauseProcessCounters
	lastPrimary, maximumPrimary                  library.PrimaryReadMeasurementSnapshot
}

func (observations *analysisPrimarySourceIOLiveObservations) record(current hlsWallPauseProcessCounters, primary library.PrimaryReadMeasurementSnapshot,
	first hlsWallPauseProcessCounters, before library.PrimaryReadMeasurementSnapshot) {
	observations.samples++
	observations.last, observations.lastPrimary = current, primary
	observations.maximum.UserTicks = max(observations.maximum.UserTicks, current.UserTicks)
	observations.maximum.SystemTicks = max(observations.maximum.SystemTicks, current.SystemTicks)
	observations.maximum.ReadChars = max(observations.maximum.ReadChars, current.ReadChars)
	observations.maximum.WriteChars = max(observations.maximum.WriteChars, current.WriteChars)
	observations.maximumPrimary.IO.Active = max(observations.maximumPrimary.IO.Active, primary.IO.Active)
	observations.maximumPrimary.IO.Background = max(observations.maximumPrimary.IO.Background, primary.IO.Background)
	observations.maximumPrimary.IO.ActiveRoots = max(observations.maximumPrimary.IO.ActiveRoots, primary.IO.ActiveRoots)
	observations.maximumPrimary.IO.ActiveDomains = max(observations.maximumPrimary.IO.ActiveDomains, primary.IO.ActiveDomains)
	observations.maximumPrimary.Owners.RegisteredOwners = max(observations.maximumPrimary.Owners.RegisteredOwners, primary.Owners.RegisteredOwners)
	observations.maximumPrimary.DomainClaims = max(observations.maximumPrimary.DomainClaims, primary.DomainClaims)
	observations.cpuGrowth = observations.cpuGrowth || current.UserTicks > first.UserTicks || current.SystemTicks > first.SystemTicks
	observations.characterIOGrowth = observations.characterIOGrowth || current.ReadChars > first.ReadChars || current.WriteChars > first.WriteChars
	observations.chargeObserved = observations.chargeObserved || primary.IO.Background > before.IO.Background && primary.IO.Active > before.IO.Active &&
		primary.IO.ActiveRoots > before.IO.ActiveRoots && primary.IO.ActiveDomains > before.IO.ActiveDomains &&
		primary.Owners.RegisteredOwners > before.Owners.RegisteredOwners && primary.DomainClaims > before.DomainClaims
}

func (observations analysisPrimarySourceIOLiveObservations) log(t *testing.T, stage string, process playbackStopAliasEncoder, before library.PrimaryReadMeasurementSnapshot) {
	t.Helper()
	t.Logf("analysis_primary_source_io stage=%s pid=%d start_tick=%d pidfd=%d samples=%d cpu_growth_observed=%t character_io_growth_observed=%t complete_charge_observed=%t first_cpu_user_ticks=%d first_cpu_system_ticks=%d first_read_chars=%d first_write_chars=%d last_cpu_user_ticks=%d last_cpu_system_ticks=%d last_read_chars=%d last_write_chars=%d max_cpu_user_ticks=%d max_cpu_system_ticks=%d max_read_chars=%d max_write_chars=%d before_active=%d before_background=%d before_owners=%d before_domain_claims=%d last_active=%d last_background=%d last_active_roots=%d last_active_domains=%d last_owners=%d last_domain_claims=%d max_observed_active=%d max_observed_background=%d max_observed_active_roots=%d max_observed_active_domains=%d max_observed_owners=%d max_observed_domain_claims=%d",
		stage, process.pid, process.startTick, process.pidfd, observations.samples, observations.cpuGrowth, observations.characterIOGrowth, observations.chargeObserved,
		process.first.UserTicks, process.first.SystemTicks, process.first.ReadChars, process.first.WriteChars,
		observations.last.UserTicks, observations.last.SystemTicks, observations.last.ReadChars, observations.last.WriteChars,
		observations.maximum.UserTicks, observations.maximum.SystemTicks, observations.maximum.ReadChars, observations.maximum.WriteChars,
		before.IO.Active, before.IO.Background, before.Owners.RegisteredOwners, before.DomainClaims,
		observations.lastPrimary.IO.Active, observations.lastPrimary.IO.Background, observations.lastPrimary.IO.ActiveRoots, observations.lastPrimary.IO.ActiveDomains,
		observations.lastPrimary.Owners.RegisteredOwners, observations.lastPrimary.DomainClaims,
		observations.maximumPrimary.IO.Active, observations.maximumPrimary.IO.Background, observations.maximumPrimary.IO.ActiveRoots, observations.maximumPrimary.IO.ActiveDomains,
		observations.maximumPrimary.Owners.RegisteredOwners, observations.maximumPrimary.DomainClaims)
}

func analysisPrimarySourceIOLogIdentity(t *testing.T, stage string, process playbackStopAliasEncoder, source os.FileInfo) {
	t.Helper()
	inherited, fdErr := os.Stat(filepath.Join("/proc", strconv.Itoa(process.pid), "fd", "3"))
	parent, parentErr := analysisPrimarySourceIOParent(process.pid)
	state, start, statErr := hlsColdPID(process.pid)
	group, groupErr := syscall.Getpgid(process.pid)
	fds := []unix.PollFd{{Fd: int32(process.pidfd), Events: unix.POLLIN}}
	ready, pollErr := unix.Poll(fds, 0)
	fdMatch := fdErr == nil && os.SameFile(source, inherited)
	t.Logf("analysis_primary_source_io stage=%s child_identified=true pid=%d expected_start_tick=%d observed_start_tick=%d process_state=%d pidfd=%d pidfd_ready=%d pidfd_revents=%d pidfd_errno=%d ppid=%d ppid_errno=%d pgid=%d pgid_errno=%d stat_errno=%d exact_fd3=%t fd3_errno=%d",
		stage, process.pid, process.startTick, start, state, process.pidfd, ready, fds[0].Revents, stoppedRetirementObservationErrno(pollErr),
		parent, stoppedRetirementObservationErrno(parentErr), group, stoppedRetirementObservationErrno(groupErr), stoppedRetirementObservationErrno(statErr),
		fdMatch, stoppedRetirementObservationErrno(fdErr))
}

func analysisPrimarySourceIORequireLiveCharge(t *testing.T, process playbackStopAliasEncoder, source os.FileInfo, before library.PrimaryReadMeasurementSnapshot) library.PrimaryReadMeasurementSnapshot {
	t.Helper()
	observations := analysisPrimarySourceIOLiveObservations{last: process.first, maximum: process.first, lastPrimary: before, maximumPrimary: before}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		current, err := hlsWallPauseReadProcess(process.pid, process.startTick)
		if err != nil {
			observations.log(t, "counter_observation_failed", process, before)
			analysisPrimarySourceIOLogIdentity(t, "counter_observation_failed", process, source)
			t.Logf("analysis_primary_source_io stage=counter_observation_failed counter_errno=%d", stoppedRetirementObservationErrno(err))
			t.Fatal("the exact analysis decoder retired before actual work and source charge were observed")
		}
		observed := library.PrimaryReadMeasurementStats()
		observations.record(current, observed, process.first, before)
		cpuGrowth := current.UserTicks > process.first.UserTicks || current.SystemTicks > process.first.SystemTicks
		characterIOGrowth := current.ReadChars > process.first.ReadChars || current.WriteChars > process.first.WriteChars
		if cpuGrowth && characterIOGrowth && observed.IO.Background > before.IO.Background && observed.IO.Active > before.IO.Active &&
			observed.IO.ActiveRoots > before.IO.ActiveRoots && observed.IO.ActiveDomains > before.IO.ActiveDomains &&
			observed.Owners.RegisteredOwners > before.Owners.RegisteredOwners && observed.DomainClaims > before.DomainClaims {
			inherited, err := os.Stat(filepath.Join("/proc", strconv.Itoa(process.pid), "fd", "3"))
			group, groupErr := syscall.Getpgid(process.pid)
			_, start, statErr := hlsColdPID(process.pid)
			if err != nil || groupErr != nil || statErr != nil || group != process.pid || start != process.startTick ||
				!os.SameFile(source, inherited) || playbackStopAliasSourceFDs(t, source) == 0 {
				observations.log(t, "source_identity_failed", process, before)
				analysisPrimarySourceIOLogIdentity(t, "source_identity_failed", process, source)
				t.Logf("analysis_primary_source_io stage=source_identity_failed fd3_errno=%d pgid=%d pgid_errno=%d observed_start_tick=%d stat_errno=%d",
					stoppedRetirementObservationErrno(err), group, stoppedRetirementObservationErrno(groupErr), start, stoppedRetirementObservationErrno(statErr))
				t.Fatal("live analysis source I/O lost its exact parent or inherited source descriptor")
			}
			observations.log(t, "live_charge_proved", process, before)
			analysisPrimarySourceIOLogIdentity(t, "live_charge_proved", process, source)
			return observed
		}
		time.Sleep(10 * time.Millisecond)
	}
	observations.log(t, "live_charge_timeout", process, before)
	analysisPrimarySourceIOLogIdentity(t, "live_charge_timeout", process, source)
	t.Fatal("actual decoder CPU and character-I/O growth never coincided with background source charge, retained owner and domain claim")
	return library.PrimaryReadMeasurementSnapshot{}
}

func TestAnalysisPrimarySourceIOProductionExecutorOwnsActualDecoder(t *testing.T) {
	runID := os.Getenv("GOBY_ANALYSIS_PRIMARY_SOURCE_IO_RUN_ID")
	if runID == "" {
		t.Skip("GOBY_ANALYSIS_PRIMARY_SOURCE_IO_RUN_ID explicitly admits the actual production Analysis source-I/O cohort")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("production Analysis source I/O requires a bounded run identity")
	}
	if testing.Short() {
		t.Skip("production Analysis source I/O requires real database and media fixtures")
	}
	for _, name := range []string{"GOBY_TEST_DATABASE_URL", "GOBY_FFMPEG", "GOBY_FFPROBE", "GOBY_INTRO_SKIPPER_FFMPEG"} {
		if os.Getenv(name) == "" {
			t.Skipf("%s is required for the production Analysis source-I/O cohort", name)
		}
	}
	before := library.PrimaryReadMeasurementStats()
	if before.IO.Active != 0 || before.IO.Background != 0 || before.IO.Queued != 0 || before.IO.ActiveRoots != 0 ||
		before.IO.ActiveDomains != 0 || before.Owners.RegisteredOwners != 0 || before.DomainClaims != 0 {
		t.Fatal("production Analysis source I/O requires an idle process-wide baseline")
	}
	official := analysisPrimarySourceIOPacedTool(t)
	f := newIntroSkipperServerFixture(t)
	sources := make([]os.FileInfo, len(f.paths))
	for index, path := range f.paths {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatal("capture the exact indexed Analysis source identities without opening a descriptor")
		}
		sources[index] = info
		if playbackStopAliasSourceFDs(t, info) != 0 {
			t.Fatal("fixture construction retained an Analysis source descriptor")
		}
	}
	if after := library.PrimaryReadMeasurementStats(); after != before {
		t.Fatal("fixture construction did not join its original-media read ownership")
	}
	started := f.start(t, introSkipperOpenGate(), true)
	if started.Task.ChildID != started.Work.ChildID || started.Task.RunID != started.Work.RunID || len(started.Work.Sources) != len(sources) {
		t.Fatal("the production executor did not receive its real admitted task and cohort")
	}
	process, sourceIndex := analysisPrimarySourceIOObserveDecoder(t, f, official, sources)
	live := analysisPrimarySourceIORequireLiveCharge(t, process, sources[sourceIndex], before)
	if _, err := f.app.taskManager.Stop(f.ctx, tasks.Actor{Principal: f.actor, Audience: identity.AdministratorNative}, started.Task.RunID); err != nil {
		t.Fatal("cancel the actual admitted Analysis run through the production task manager")
	}
	executionErr := f.finish(t, started, tasks.RunCancelled)
	if !errors.Is(executionErr, context.Canceled) || errors.Is(executionErr, media.ErrProcessRetirementUnknown) {
		t.Fatalf("actual Analysis cancellation did not return a joined, known retirement: %v", executionErr)
	}
	deadline := time.Now().Add(5 * time.Second)
	retired := false
	for time.Now().Before(deadline) {
		exited, reaped, groupClosed, known, facts := stoppedEncoderRetirementObservation(process)
		if !known {
			t.Fatalf("the independently pinned Analysis retirement became unknown: %+v", facts)
		}
		if exited && reaped && groupClosed {
			retired = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !retired {
		t.Fatal("the actual Analysis decoder did not exit, reap and retire its owned group")
	}
	for _, source := range sources {
		if playbackStopAliasSourceFDs(t, source) != 0 {
			t.Fatal("exact Analysis source descriptors survived executor join and independent decoder retirement")
		}
	}
	cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	closeErr := f.app.Close(cleanup)
	cancel()
	if closeErr != nil {
		t.Fatal("join the actual task manager, Analysis runtime and Store before asserting idle ownership")
	}
	if after := library.PrimaryReadMeasurementStats(); after != before {
		t.Fatalf("joined Analysis cancellation retained source ownership: before=%+v after=%+v", before, after)
	}
	t.Logf("analysis_primary_source_io run_id=%s actual_task_cohort=true official_decoder=true exact_source_fd3=true live_background=%d live_active=%d live_owners=%d live_domain_claims=%d cpu_and_character_io_growth=true task_cancelled=true executor_joined=true pinned_exit_reap_group=true source_fds=0 idle_after_store_join=true",
		runID, live.IO.Background, live.IO.Active, live.Owners.RegisteredOwners, live.DomainClaims)
}

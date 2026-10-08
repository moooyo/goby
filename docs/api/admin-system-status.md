# Administrator system status

`GET /admin/v1/system/status` returns live host observations for the dashboard.
It requires the current native administrator cookie and uses the existing
administrator authorization boundary. Emby logins and application keys do not
authorize this endpoint. Responses have `Cache-Control: no-store`. Query
parameters, including an empty `?`, are rejected with `400 invalid_query`.
There is no caller-selected filesystem path, command, or sampling interval.

The response uses this shape (numbers below are illustrative):

```json
{
  "Timestamp": "2026-09-30T01:02:03Z",
  "UptimeSeconds": 123,
  "Host": { "OS": "linux", "Architecture": "amd64", "CPUCount": 8 },
  "CPU": { "UsagePercent": 23.5, "Load1": 1.42, "Load5": 0.98, "Load15": 0.87 },
  "Memory": {
    "TotalBytes": 17179869184,
    "UsedBytes": 6553600000,
    "CachedBytes": 3650722201,
    "SwapUsedBytes": 0
  },
  "Storage": {
    "Complete": true,
    "TotalBytes": 16000000000000,
    "UsedBytes": 11200000000000,
    "Volumes": [
      { "Path": "/srv/media", "Available": true, "TotalBytes": 16000000000000, "UsedBytes": 11200000000000 }
    ]
  },
  "Transcoding": { "Available": true, "Active": 2, "Limit": 4, "HardwareActive": 1, "SoftwareActive": 1 }
}
```

## Observation semantics

- `Timestamp` identifies the start of the reported host sample. Host polls
  trigger at most one sample per second, with at most one outstanding sampling
  worker. Operating-system I/O never runs in an HTTP request or under the
  collector's mutex. A blocked NFS/SMB mount therefore cannot hold status
  requests or accumulate sampling workers. Transcoding counters are read when
  producing the response. No telemetry subprocess is created.
- Requests immediately receive the last completed sample while it is at most
  fifteen seconds old. This bound exceeds the dashboard's five-second polling
  interval. Before the initial sample completes, or after the prior sample
  expires, host measurements are `null` and configured volumes are unavailable.
  In that case `Timestamp` identifies the last sampling attempt; host identity
  and uptime remain available. A slow sample retains its original start time
  and cannot masquerade as fresh when its filesystem call finally returns.
  The next poll can admit a new sample after that worker finishes. Shutdown
  prevents further samples and ignores late publication without waiting for
  an uninterruptible kernel filesystem operation.
- `UptimeSeconds` is the elapsed lifetime of this server's initialized host
  collector, beginning after application startup, rather than the host's boot
  time. `Host.CPUCount` is the logical CPU count reported by the Go runtime.
- `CPU.UsagePercent` is the non-idle fraction between successive observations,
  expressed from 0 to 100. It is `null` until two valid counter observations
  at least one second apart exist. Counter failures and resets invalidate the
  interval. Linux includes I/O wait in idle time and excludes double-counted
  guest time. Load values are Linux one-, five-, and fifteen-minute averages.
- Memory describes the OS host, not the Goby process or a container resource
  limit. Linux reads `/proc/meminfo`: used is `MemTotal - MemAvailable`, cached
  is `Cached + SReclaimable - Shmem`, and swap used is `SwapTotal - SwapFree`.
  Missing fields remain `null`.
- `Storage.Volumes` contains one entry per distinct configured media directory,
  including unavailable directories. Each entry describes its backing
  filesystem, not the bytes occupied by that directory. Aggregate capacity
  counts a backing filesystem once even if several directories use it. Linux
  identifies the filesystem by device ID, including bind mounts, and reports
  filesystem blocks and free blocks, including space reserved from ordinary
  users.
- If any configured directory cannot be read, `Storage.Complete` is false and
  aggregate `TotalBytes` and `UsedBytes` are `null`; readable per-directory
  observations remain available. An empty directory configuration has an empty
  array, zero aggregate bytes, and `Complete: true`. It does not substitute the
  system disk. Overflow and impossible counters are treated as unavailable.
- `Transcoding` counts occupied conversion slots, including jobs whose
  cancellation has been requested until they actually release their slots.
  `Limit` is the manager's effective `MaxJobs`, not a queue or session count.
  Hardware jobs use hardware decoding, encoding, or GPU video filters; all
  other occupied jobs are software jobs. Queued and completed jobs, playback
  sessions, and a diagnostic slot reservation are not active conversion jobs.
  `Available` indicates whether conversion counters can be read, independently
  of the conversion engine's health reported by `/admin/v1/overview`.
- A disabled conversion engine has `Available: false` and all four counts zero.
  An enabled engine that cannot report metrics has `Available: false` and all
  four counts `null`. Unavailable host metrics also remain `null` rather than
  becoming zero-valued success.

The endpoint is operational telemetry, not a readiness decision or a hardware
capability proof. Partial host observations still produce HTTP 200; an absent
collector returns `503 system_status_unavailable`.

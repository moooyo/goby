# Frozen intro corpus runner

`intro-corpus.go` is a Linux-only, single-file operator for a frozen inventory.
It extracts production features, preserves per-source outcomes, and runs the
detector with bounded diagnostics. It does not download sources, assign labels,
change the source inventory, or publish application markers.

Run it in the designated remote verification environment. The existing flags
`-sources`, `-labels`, `-labels-sha256`, `-series`, `-features`,
`-ffmpeg`, `-ffprobe`, and `-fingerprint` retain their meaning.

## Extraction and resume

Start with a new absolute feature directory:

```sh
go run scripts/test-env/intro-corpus.go -mode extract \
  -sources /private/frozen/sources.json -series selected-series \
  -labels /private/frozen/labels.json -labels-sha256 FROZEN_LABEL_SHA256 \
  -features /private/run/features
```

Every selected source receives an independent durable attempt:

| Status | Meaning |
| --- | --- |
| `extracted` | Complete audio and visual features match the admitted source, tools, and extraction profile. |
| `excluded` | Probe evidence confirms a missing audio or video stream. This is an unevaluable source, not a successful negative. |
| `failed` | Source reading, probing, extraction, or evidence verification failed. Other sources continue. |

Use the same command with `-resume` to revalidate and reuse successful or
excluded entries. Use `-resume -retry-failed` to append new attempts for failed
entries. Sources not yet attempted are processed by either resume form.

Resume verifies the complete inventory bytes, label bytes, source SHA-256 and
filesystem identity, feature-file SHA-256, extraction profile, tool hashes,
fingerprint protocol metadata, and extraction limits. A changed global identity
requires a new feature directory. Old feature files without a checkpoint are
not automatically imported.

A cache entry that fails revalidation is recorded as a new failure. It is not
silently repaired in that run, even with `-retry-failed`; a later explicit
retry creates a new extraction attempt. Existing features and original failures
remain retained. The first feature is `ID.json`; later attempts use
`ID.attempt-N.json`.

The private, mode-0600 checkpoint retains source paths, raw errors, exclusion
probe evidence, and every attempt. Keep the entire feature directory private.
Progress output includes validated case IDs, statuses, and reason codes only.
A Linux advisory lock prevents concurrent operators from modifying the same
feature directory and is released automatically when a process exits.

## Analysis

```sh
go run scripts/test-env/intro-corpus.go -mode analyze \
  -sources /private/frozen/sources.json -series selected-series \
  -labels /private/frozen/labels.json -labels-sha256 FROZEN_LABEL_SHA256 \
  -features /private/run/features -output /private/run/result.json
```

The output path must be new. The report preserves accounting for every selected
case. Excluded sources remain in `Cases` but do not enter the matcher. A failed,
missing, corrupt, stale, or duplicate independent source blocks matching.
At least three independent eligible episodes are required, or the larger
configured `MinSupport`. This prevents an incomplete cohort from silently
becoming a different evaluation population.

`Result` remains the production detector result; `Diagnostics` explains
intermediate decisions. `MatcherRan` distinguishes blocked runs from
completed negative observations. Once the frozen inventory and labels have been
admitted, failures before matcher admission also write a blocked report.
Malformed/unreadable inventories or labels cannot produce trusted case
accounting and return a reason code without a report.

`-options PATH` accepts a complete detector-options JSON document only when
every selected source has role `calibration`. It does not permit experiments
on held-out sources, including sources subsequently excluded from extraction.
The exact options are included in the report.

`-visual-experiment` adds a separate `VisualExperiment` observation after
the same corpus admission checks. It leaves the production `Result` and
`Diagnostics` unchanged. Visual experiment groups are not application skip
markers and must not be published as such. The flag is off by default.

## Focused verification

The entry point has a build-ignore tag to avoid mixing it with other standalone
operators. Pass both files explicitly on the remote environment:

```sh
go test -count=1 scripts/test-env/intro-corpus.go scripts/test-env/intro-corpus_test.go
go test -race -count=1 scripts/test-env/intro-corpus.go scripts/test-env/intro-corpus_test.go
```

The behavioral tests cover continued extraction, missing streams, private
failure retention, interruption recovery, exact resume reuse, explicit retries,
source/cache/profile/tool/label drift, blocked accounting, independent support,
calibration-only options, non-regular sources, concurrent admission, and
separation of optional visual experiments from production results.

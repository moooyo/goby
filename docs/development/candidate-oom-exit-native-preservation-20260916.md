# Native preservation checkpoint, September 16

One bounded native observer completed successfully for both stopped candidates. The actual independent review receipt has status `native_observation_and_evidence_reviewed`. **Both applications remain failed with MainPID zero; application recovery and a new runtime authority are not established.**

The scope is `/opt/goby-test/candidate-oom-exit-recovery-20260916/native-log-r01`. [The checkpoint metadata](candidate-oom-exit-native-preservation-20260916.json) records exact admission, source, execution, result, review, and reused readonly-checkpoint pins.

| Measure | Observed | Limit |
|---|---:|---:|
| Observer invocations | 1 | 1 |
| Observed and evidence files | 51 | 64 |
| Bytes read / evidence written | 2,089,292 / 51,637 | 32 MiB combined |
| Evidence files written | 16 | Included above |
| Observer duration | 51 ms | 60 seconds |

Lifecycle/control relations, staged-generation shape, configuration hashes, master metadata, backup catalog/object metadata, and saved SQL bindings matched. The existing readonly evidence was reused unchanged; there was no new SQL, expiry evaluation, store write, store open, application start, HTTP request, or service change.

The independent saved-evidence review checked 16 evidence files, 14 raw pins, and all 32 metadata command streams. All 16 metadata commands closed; protected state matched exactly before and after, and the shared lock was released. The full worker had closed before admission.

This result does not prove every historical native-file identity, encrypted backup contents, master/key contents, physical integrity, durability, application health, or recovery completion. Backup bodies and master contents were not read or content-hashed; configuration/environment contents were not decoded.

The diagnostics/log/cache phase has **not run**. It is deliberately deferred until actual restart readiness to keep its new one-hour start window fresh. No historical window or this native result authorizes a restart.

# Remote verification capacity

Before preparing a new verification environment on `test-env`, read
`docs/development/test-env-capacity-policy.md`.

Reuse shared Go build and module caches for ordinary runs. Respect an explicitly
selected isolated environment; create its private build cache empty and budget
it rather than copying caches into every task or source variant.

During authorized task closeout, reclaim inactive private compiler caches and
scratch after the workers have exited. Retain source, data and raw evidence
separately, and preserve the selected filesystem for media test fixtures.

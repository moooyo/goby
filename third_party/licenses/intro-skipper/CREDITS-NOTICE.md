# Intro Skipper default credits pass derived code

`internal/creditsskipper` mechanically translates the default CreditsPass
chapter, current black-frame, entropy fallback, combination, and boundary
adjustment stages from Intro Skipper 12.0.4.0, pinned to commit
`6e0cb179007ac4c16cd9f358e9a617e791e9bf06`.

Upstream repository: <https://github.com/intro-skipper/intro-skipper>

The derived package and upstream golden tests are **GPL-3.0-only**. The complete
license is preserved in [LICENSE](LICENSE). This notice neither selects a
license for unrelated Goby source nor asserts completion of distribution review.

Original source and test copyright statements identify:

- 2022 ConfusedPolarBear
- 2024-2026 rlauuzo
- 2024-2026 AbandonedCart
- 2024-2026 Kilian von Pflugk

The newer visual, combination, and adjustment helpers carry the narrower
2025/2026 or 2026 notices retained in their translated files. The exact upstream
files, commit, and source SHA256 values are recorded in
[credits-source-pin.json](credits-source-pin.json).

Port changes are Go data types and an injected process-free probe interface,
explicit source-coordinate contracts, stricter failure/cancellation propagation,
bounded stored evidence, and independent provenance/structural validation.
Successful default detection, combination, and adjustment decisions remain the
pinned upstream rules, including adjustment after chapter-bounded combination.
The package README records deliberate admission/error-handling differences and
excluded custom/legacy modes. The existing `internal/introskipper` audio matcher
and its independent source pin remain separately attributed.

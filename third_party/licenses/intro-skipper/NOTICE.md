# Intro Skipper derived code

`internal/introskipper/core.go` is a mechanical Go translation of the
Introduction raw-candidate matching logic in Intro Skipper 12.0.4.0, pinned to
commit `6e0cb179007ac4c16cd9f358e9a617e791e9bf06`.

Upstream repository: <https://github.com/intro-skipper/intro-skipper>

The derived package and its tests are licensed under **GPL-3.0-only**. The full
license text is provided in `LICENSE`. This notice does not declare or change the
license of unrelated Goby code, and does not assert that a combined distribution
has completed a licensing review.

The pinned upstream matching source identifies these copyright holders:

- 2022 ConfusedPolarBear
- 2024-2026 rlauuzo
- 2024-2026 AbandonedCart
- 2024-2026 Kilian von Pflugk

`TickConversions.cs` also credits 2026 rlauuzo and 2026 AbandonedCart. Configuration
and queue source files include additional upstream notices; consult the pinned
source files listed in `source-pin.json` for their complete headers.

Changes in this port include Go types and storage contracts, context cancellation,
bounded input and work admission, actual winning-pair provenance, and exported
configuration validation. Core successful matching decisions preserve the pinned
raw-candidate behavior. Optional boundary adjustment is not included.

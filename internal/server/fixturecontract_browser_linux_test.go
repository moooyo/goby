//go:build linux && goby_embed_admin && goby_browser_integration

package server

// Browser fixture declarations are compiled only with their exact existing
// fixture tags. They retain original optional version methods and promise that
// direct source use joins before return. All source-bearing children must use
// and join the actual media gateway; the marker is not an OS certificate.

// The stream fixture joins before the callback adds local embedded music facts.
func (phase3BrowserProber) ProbeFileJoinedContract() bool { return true }

// The third-call cancellation gate ends inside the dynamic override. Other
// calls synchronously delegate to the real prober under the collector context.
// This explicit marker prevents its promoted Owned method from bypassing the
// gate/counters; actual real-child retirement remains the cohort's obligation.
func (prober *refreshBrowserProber) ProbeFileJoinedContract() bool { return prober != nil }

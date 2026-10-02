package server

import (
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

const maxGeneratedWindowRedirectPins = maxHLSSessions * maxHLSProducers * transcode.MaxHLSRenditions * hlsGeneratedWindowResourcesPerRendition

// retainGeneratedWindowPinLocked requires session.mu. The pin index never
// acquires a session mutex. The fixed bound allows independently charged MAP
// and media readers for each retained fMP4 rendition; producer counts stay fixed.
// A retained handle is borrowed ownership; even another valid registration of
// the same manager job cannot transfer it into a second graph.
func (h *hlsRuntime) retainGeneratedWindowPinLocked(session *hlsSession, pin *transcode.ReadHandle, expectedIdentity string) error {
	if session == nil || pin == nil || expectedIdentity == "" {
		return transcode.ErrInvalidOptions
	}
	h.generatedWindowPinMu.Lock()
	defer h.generatedWindowPinMu.Unlock()
	if h.generatedWindowPinOwners[pin] != nil {
		return transcode.ErrOutputUnavailable
	}
	if len(h.generatedWindowPinOwners) >= maxGeneratedWindowRedirectPins {
		return transcode.ErrBusy
	}
	// Cleanup and claim share this fence. An alias that was closed after an
	// earlier stat cannot become a live registry entry when cleanup returns.
	stat, err := pin.Stat()
	if err != nil {
		return err
	}
	identity, err := media.VideoSeekSourceIdentity(stat)
	if err != nil || identity != expectedIdentity {
		return transcode.ErrInvalidInput
	}
	if h.generatedWindowPinOwners == nil {
		h.generatedWindowPinOwners = make(map[*transcode.ReadHandle]*hlsSession)
	}
	h.generatedWindowPinOwners[pin] = session
	return nil
}

func (h *hlsRuntime) generatedWindowPinOwner(pin *transcode.ReadHandle) *hlsSession {
	h.generatedWindowPinMu.Lock()
	defer h.generatedWindowPinMu.Unlock()
	return h.generatedWindowPinOwners[pin]
}

// releaseGeneratedWindowPinLocked requires the owner's session.mu. Retain the
// index until actual descriptor cleanup finishes. A foreign borrowed alias
// cannot close the original graph's reader or remove its ownership record.
func (h *hlsRuntime) releaseGeneratedWindowPinLocked(session *hlsSession, pin *transcode.ReadHandle) {
	if pin == nil {
		return
	}
	h.generatedWindowPinMu.Lock()
	defer h.generatedWindowPinMu.Unlock()
	owner := h.generatedWindowPinOwners[pin]
	if owner != nil && owner != session {
		return
	}
	_ = pin.Close()
	delete(h.generatedWindowPinOwners, pin)
}

// Rejection consumes only fresh caller ownership. All retained incoming
// handles, including cross-registration aliases, remain their owner's charge.
func (h *hlsRuntime) closeUnretainedGeneratedWindowPin(_ *hlsSession, pin *transcode.ReadHandle) {
	if pin == nil {
		return
	}
	h.generatedWindowPinMu.Lock()
	defer h.generatedWindowPinMu.Unlock()
	if h.generatedWindowPinOwners[pin] == nil {
		_ = pin.Close()
	}
}

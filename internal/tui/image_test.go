package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Chmgx81/tilde/internal/llm"
)

// TestImageAttachAndSubmit covers the ctrl+v lifecycle: clipboard
// read, placeholder insert, toast, submit routing, and the honest
// failure when there is no image.
func TestImageAttachAndSubmit(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	// No image on the clipboard: a toast says so, nothing attaches.
	noImage := func() ([]byte, error) { return nil, ErrNoImage }
	restore := swapClipboard(noImage)
	defer restore()
	m.attachClipboardImage()
	if len(m.images) != 0 {
		t.Fatalf("attached with no image: %v", m.images)
	}
	if !strings.Contains(m.toast, "no image") {
		t.Errorf("no-image toast missing: %q", m.toast)
	}

	// A real read: placeholder inserted, image stored, toast fires.
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	swapClipboard(func() ([]byte, error) { return png, nil })
	m.attachClipboardImage()
	if len(m.images) != 1 || m.images["[Image #1]"].MimeType != "image/png" {
		t.Fatalf("image not stored: %v", m.images)
	}
	if !strings.Contains(m.composer.Value(), "[Image #1]") {
		t.Errorf("placeholder missing from composer: %q", m.composer.Value())
	}
	if !strings.Contains(m.toast, "attached") {
		t.Errorf("attach toast missing: %q", m.toast)
	}

	// A second image numbers its placeholder.
	m.attachClipboardImage()
	if _, ok := m.images["[Image #2]"]; !ok {
		t.Errorf("second image not numbered: %v", m.images)
	}

	// ctrl+v reaches the attach path through the real key handler.
	m.images, m.imageSeq, m.toast = nil, 0, ""
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if len(m.images) != 1 {
		t.Errorf("ctrl+v did not attach: %v", m.images)
	}

	// Submit consumes the pending images in placeholder order and
	// clears the attachment state.
	got := m.pendingImages()
	if len(got) != 1 || string(got[0].Data) != string(png) {
		t.Fatalf("pendingImages = %v", got)
	}
	if len(m.images) != 0 || m.imageSeq != 0 {
		t.Errorf("attachment state not cleared: %v %d", m.images, m.imageSeq)
	}
}

// TestImageSubmitRoutesToOrchestrator proves the images actually ride
// with the turn: a queued follow-up carries them, the drain starts
// the turn with them, and the provider sees them in the request.
func TestImageSubmitRoutesToOrchestrator(t *testing.T) {
	dir := t.TempDir()
	rounds := [][]llm.ChatEvent{{{Type: llm.TextEvent, Text: "done"}}}
	m, fp := newText(t, dir, rounds)
	swapClipboard(func() ([]byte, error) { return []byte("img"), nil })

	m.working = true
	m.attachClipboardImage()
	m.Update(altEnterKey())
	if len(m.queue) != 1 || len(m.queue[0].images) != 1 {
		t.Fatalf("queued follow-up lost its image: %+v", m.queue)
	}
	if len(m.images) != 0 {
		t.Errorf("images not consumed by the queue submit: %v", m.images)
	}

	// The drain passes the images into the new turn, and the provider
	// receives them on the last user message.
	m.turnEnded()
	waitFor(t, func() bool { return fp.requestCount() == 1 })
	fp.mu.Lock()
	msgs := fp.gotRequests[0].Messages
	fp.mu.Unlock()
	var lastUser *llm.Message
	for i := range msgs {
		if msgs[i].Role == "user" {
			lastUser = &msgs[i]
		}
	}
	if lastUser == nil || len(lastUser.Images) != 1 || string(lastUser.Images[0].Data) != "img" {
		t.Fatalf("image did not ride with the request: %+v", lastUser)
	}
	if !strings.Contains(lastUser.Content, "[Image #1]") {
		t.Errorf("placeholder text missing from the message: %q", lastUser.Content)
	}
}

// TestImageCapRejectsHugeClipboard pins the loud refusal: an
// oversized image errors instead of shipping megabytes.
func TestImageCapRejectsHugeClipboard(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	swapClipboard(func() ([]byte, error) { return make([]byte, imageBytesCap+1), nil })
	m.attachClipboardImage()
	if len(m.images) != 0 {
		t.Errorf("oversized image attached: %v", m.images)
	}
	if !strings.Contains(m.toast, "cap") {
		t.Errorf("cap toast missing: %q", m.toast)
	}
}

// swapClipboard fakes the platform read for the test and returns a
// function restoring the production reader.
func swapClipboard(fake func() ([]byte, error)) func() {
	orig := readClipboard
	readClipboard = fake
	return func() { readClipboard = orig }
}

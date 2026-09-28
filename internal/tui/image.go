package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/Chmgx81/tilde/internal/llm"
)

// imageBytesCap rejects absurd clipboard payloads loudly instead of
// shipping a 50 MiB screenshot into a token stream.
const imageBytesCap = 8 << 20

// ErrNoImage is the honest "nothing to attach" verdict.
var ErrNoImage = errors.New("no image on the clipboard")

// clipboardSource is one platform clipboard tool invocation that can
// dump image bytes in one format.
type clipboardSource struct {
	name string
	args []string
}

// clipboardSources, tried in order. PNG first (the common case), then
// JPEG — a clipboard holding only a JPEG screenshot would fail the
// PNG request and still attach. Wayland, X11, macOS; whichever exists
// and returns bytes wins — terminals themselves cannot deliver image
// bytes through bracketed paste.
var clipboardSources = []clipboardSource{
	{"wl-paste", []string{"-t", "image/png"}},
	{"wl-paste", []string{"-t", "image/jpeg"}},
	{"xclip", []string{"-selection", "clipboard", "-t", "image/png", "-o"}},
	{"xclip", []string{"-selection", "clipboard", "-t", "image/jpeg", "-o"}},
	{"pngpaste", []string{"-"}}, // macOS: PNG always
}

// readClipboardImage returns the clipboard image bytes, or ErrNoImage
// when every source is unavailable or empty. A source that errors is
// skipped, not fatal — the next one may work.
func readClipboardImage() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, src := range clipboardSources {
		cmd := exec.CommandContext(ctx, src.name, src.args...)
		out, err := cmd.Output()
		if err != nil || len(out) == 0 {
			continue
		}
		if len(out) > imageBytesCap {
			return nil, fmt.Errorf("clipboard image is %s — over the %s cap",
				humanBytes(len(out)), humanBytes(imageBytesCap))
		}
		return out, nil
	}
	return nil, ErrNoImage
}

// detectImageMime reports the actual format of the bytes from their
// magic signature — png, jpeg, gif, or webp — or "" when they are not
// a recognized image. The bytes are already in hand, so the label is
// read from them, never assumed from the tool that produced them.
func detectImageMime(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return "image/png"
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return "image/gif"
	case len(data) >= 12 && bytes.HasPrefix(data, []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "image/webp"
	}
	return ""
}

func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%.0f KiB", float64(n)/(1<<10))
	}
}

// readClipboard is the seam tests swap to fake a platform clipboard;
// production code points it at readClipboardImage.
var readClipboard = readClipboardImage

// attachClipboardImage is ctrl+V: read the clipboard, store the
// image, and insert a numbered placeholder at the cursor — the same
// token pattern large pastes use, so the composer stays one surface.
func (m *Model) attachClipboardImage() {
	data, err := readClipboard()
	if err != nil {
		m.showToast(err.Error())
		return
	}
	// The cap is enforced at the consumer, not only the platform
	// reader, so the bound holds whatever produced the bytes.
	if len(data) > imageBytesCap {
		m.showToast(fmt.Sprintf(
			"clipboard image is %s — over the %s cap",
			humanBytes(len(data)), humanBytes(imageBytesCap)))
		return
	}
	mime := detectImageMime(data)
	if mime == "" {
		m.showToast("clipboard content is not a recognized image (png, jpeg, gif, webp)")
		return
	}
	if m.images == nil {
		m.images = make(map[string]llm.Image)
	}
	m.imageSeq++
	token := fmt.Sprintf("[Image #%d]", m.imageSeq)
	m.images[token] = llm.Image{MimeType: mime, Data: data}
	m.composer.InsertString(token)
	m.showToast(fmt.Sprintf("attached %s %s (ctrl+v again for more)",
		humanBytes(len(data)), shortMime(mime)))
}

// shortMime drops the "image/" prefix for toasts — "attached 12 KiB
// png" reads better than the full type.
func shortMime(mime string) string {
	const p = "image/"
	if len(mime) > len(p) && mime[:len(p)] == p {
		return mime[len(p):]
	}
	return mime
}

// pendingImages returns the attached images in placeholder order and
// clears the attachment state — submit consumes them like paste
// tokens.
func (m *Model) pendingImages() []llm.Image {
	if len(m.images) == 0 {
		return nil
	}
	out := make([]llm.Image, 0, len(m.images))
	for i := 1; i <= m.imageSeq; i++ {
		if img, ok := m.images[fmt.Sprintf("[Image #%d]", i)]; ok {
			out = append(out, img)
		}
	}
	m.images, m.imageSeq = nil, 0
	return out
}

# Phase 22 — Image paste + vision

## Goal

Ctrl+V attaches the clipboard image to the message; the model
receives it as an `image_url` content part and can see it. Terminals
cannot deliver image bytes through bracketed paste, so the clipboard
is read through the platform tool — Claude Code's approach — and the
composer shows a `[Image #N]` placeholder, the same token pattern
large pastes use.

## Non-Goals

- Drag-and-drop: terminals deliver that as filename text; the
  existing `@` mention covers reading the file.
- Rendering images in the transcript (terminal image protocols are
  a separate rabbit hole; the placeholder is honest).
- Image tools (screenshot capture, resize) — attach what is on the
  clipboard, nothing more.

## Approach

**The wire.** `llm.Image{MimeType, Data}` on `Message.Images`; when a
user message carries images, `content` becomes the OpenAI parts
array — `{"type":"text"}` then one `{"type":"image_url","image_url":
{"url":"data:<mime>;base64,…"}` per image. Text-only messages keep
the plain string form (compat: every server accepts it, and tool
messages must stay strings).

**The clipboard.** `clipboardImage()` tries, in order: `wl-paste -t
image/png` (Wayland), `xclip -selection clipboard -t image/png -o`
(X11), `pngpaste -` (macOS). First one that returns bytes wins; none
available → a toast says so, never a silent no-op. Byte cap (8 MiB)
rejects absurd pastes loudly.

**The composer.** Ctrl+V (only when the composer isn't in a special
mode) stores the image, inserts `[Image #N]` at the cursor, and
toasts. Submit replaces nothing — the placeholder stays as visible
text the model sees alongside the real parts. Images clear on submit
like paste tokens do. Headless: no clipboard in `-p` mode, so no
image path there.

**History.** Images persist on the history message for the session
(vision models accept repeats; a resumed session re-sends what was
sent — the session already stores full history, and this keeps the
promise that what the model saw once it sees again).

## Edge cases

- Non-image clipboard: the tools above fail or return nothing →
  toast "no image on the clipboard".
- Huge images: > 8 MiB is refused with the size in the toast.
- Providers without vision: they 4xx the request; the error
  surfaces in the transcript as a turn error, as any provider error
  does.
- Multiple images: numbered placeholders, sent in order.

## Test plan

- llm: toWire builds the parts array with a correct data URL and
  leaves text-only and tool messages as strings.
- tui: ctrl+v attaches (fake reader injected), placeholder appears,
  toast fires; submit routes images to the orchestrator; no image →
  error toast.
- PTY, live: a fake `wl-paste` on PATH emits a tiny PNG; the scripted
  session attaches it, and the fixture server inspects the request
  body, asserts an `image_url` part with the right mime, and answers
  accordingly — end to end, no mocks in between.

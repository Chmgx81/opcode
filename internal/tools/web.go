package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// WebFetch retrieves a URL and returns readable text — WebFetch's
// shape from the reference agents. Action-Allowed tier: network
// egress is a trust boundary the sandbox does not cover (Codex's
// sandbox denies network by default), so a fetch asks in plan and
// build mode and runs free only in full-auto.
type WebFetch struct{}

func (WebFetch) Name() string { return "web_fetch" }

func (WebFetch) Description() string {
	return "Fetch an http(s) URL and return its readable text (HTML is stripped to text, script/style dropped), for documentation, API references, and error lookups. Redirects are followed up to 5; responses cap at 256 KiB; non-text content types report their size instead of dumping bytes."
}

func (WebFetch) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {"type": "string", "description": "The absolute http:// or https:// URL to fetch"}
		},
		"required": ["url"]
	}`)
}

func (WebFetch) Tier() Tier { return TierActionAllowed }

const (
	webFetchTimeout   = 20 * time.Second
	webFetchMaxBytes  = 256 << 10
	webFetchMaxRedirs = 5
)

// WebFetchUA is the fetch user agent; cmd/tilde sets it to the
// binary's real version so the UA never disagrees with --version.
var WebFetchUA = "tilde/0.3 (+https://github.com/Chmgx81/tilde)"

// blockedFetchHosts are the hostnames and address blocks the model
// may not fetch: the loopback (tilde itself or local services), the
// private LAN ranges (a model probing the user's network is the same
// exfiltration class), link-local (including the cloud metadata
// endpoints every provider warns about), and the unspecified address.
// The model's input is untrusted; the machine's own interfaces and
// its neighbors are not its reading material.
//
// Name resolution is the caller's, not ours: a hostname that resolves
// to a blocked range still passes this check (DNS-rebinding TOCTOU),
// so this is a backstop for literal addresses, not a substitute for
// the approval gate — fetches still ask in plan and build mode.
func checkFetchTarget(rawURL string) error {
	// The user's explicit opt-in (a developer pointing the model at
	// a local dev server); the default is the safe refusal.
	if os.Getenv("TILDE_ALLOW_LOCAL_FETCH") != "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("web_fetch: %w", err)
	}
	host := u.Hostname()
	blocked := host == "localhost"
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			blocked = true
		}
	}
	if blocked {
		return fmt.Errorf("web_fetch: refusing to fetch %s — loopback, private, and link-local addresses are off-limits (set TILDE_ALLOW_LOCAL_FETCH=1 to allow local fetches)", host)
	}
	return nil
}

func (WebFetch) Execute(ctx context.Context, args string) (string, error) {
	var a struct {
		URL string `json:"url"`
	}
	if err := parseArgs(args, &a); err != nil {
		return "", err
	}
	u := strings.TrimSpace(a.URL)
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return "", fmt.Errorf("web_fetch: url must be absolute http:// or https://")
	}
	if err := checkFetchTarget(u); err != nil {
		return "", err
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, webFetchTimeout)
		defer cancel()
	}
	client := &http.Client{
		Timeout: webFetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= webFetchMaxRedirs {
				return fmt.Errorf("web_fetch: stopped after %d redirects", webFetchMaxRedirs)
			}
			// Every redirect hop is re-validated: an external URL
			// bouncing to the machine's own services is the classic
			// SSRF pivot.
			return checkFetchTarget(req.URL.String())
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("web_fetch: %w", err)
	}
	req.Header.Set("User-Agent", WebFetchUA)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("web_fetch: %w", err)
	}
	defer resp.Body.Close()

	ctype := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ctype, "text/") &&
		!strings.Contains(ctype, "json") && !strings.Contains(ctype, "xml") &&
		!strings.Contains(ctype, "javascript") {
		return fmt.Sprintf("%s %s — %s (%d bytes, not fetched as text)",
			resp.Status, u, ctype, resp.ContentLength), nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, webFetchMaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("web_fetch: %w", err)
	}
	truncated := len(body) > webFetchMaxBytes
	if truncated {
		body = body[:webFetchMaxBytes]
	}
	out := string(body)
	if strings.Contains(ctype, "html") {
		out = htmlToText(out)
	}
	out = strings.TrimSpace(out)
	if truncated {
		out += "\n… (truncated at 256 KiB)"
	}
	if resp.StatusCode >= 400 {
		return fmt.Sprintf("web_fetch: %s\n\n%s", resp.Status, out), nil
	}
	return out, nil
}

// htmlToText extracts readable text from HTML: script and style
// blocks are dropped entirely, tags stripped, entities decoded,
// and whitespace collapsed. A reader, not a renderer. Case is
// preserved — only the tag search is case-insensitive.
func htmlToText(s string) string {
	lower := strings.ToLower(s)
	for _, tag := range []string{"script", "style"} {
		for {
			start := indexTag(lower, "<"+tag)
			if start < 0 {
				break
			}
			end := indexTag(lower, "</"+tag)
			if end < 0 {
				s, lower = s[:start], lower[:start]
				break
			}
			gt := strings.IndexByte(lower[end:], '>')
			if gt < 0 {
				s, lower = s[:start], lower[:start]
				break
			}
			stop := end + gt + 1
			s = s[:start] + s[stop:]
			lower = lower[:start] + lower[stop:]
		}
	}
	// Block-level tags become line breaks so headings and
	// paragraphs survive. pos skips past each handled tag — the
	// newline lands in front of it, so re-searching from before it
	// would find it forever.
	for _, tag := range []string{"p", "br", "h1", "h2", "h3", "h4", "li", "tr", "div"} {
		pos := 0
		for {
			i := indexTag(lower[pos:], "<"+tag)
			if i < 0 {
				break
			}
			i += pos
			s = s[:i] + "\n" + s[i:]
			lower = lower[:i] + "\n" + lower[i:]
			pos = i + 2 + len(tag) // past the newline and "<name"
		}
	}
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	lines := strings.Split(html.UnescapeString(b.String()), "\n")
	var out []string
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// indexTag finds "<name" in lower where it opens the tag (followed
// by whitespace, ">", or "/") — so "<p" never matches "<pre".
func indexTag(lower, name string) int {
	for i := 0; ; {
		j := strings.Index(lower[i:], name)
		if j < 0 {
			return -1
		}
		i += j
		if i+len(name) >= len(lower) {
			return i
		}
		switch lower[i+len(name)] {
		case ' ', '\t', '\n', '\r', '>', '/':
			return i
		}
		i += len(name)
	}
}

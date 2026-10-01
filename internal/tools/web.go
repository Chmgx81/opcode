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

// localFetchAllowed is the user's explicit opt-in (a developer
// pointing the model at a local dev server). It disables both the
// spelling check and the dial-time check, because an allowlist that
// only one of them honors is not an allowlist.
func localFetchAllowed() bool { return os.Getenv("TILDE_ALLOW_LOCAL_FETCH") != "" }

// checkFetchTarget refuses the host spellings the model uses to name
// the machine itself: the loopback under any spelling (LOCALHOST,
// localhost. with its FQDN root dot, localhost.localdomain, ::1), the
// private LAN ranges, link-local (including every cloud metadata
// endpoint), the CGNAT range Tailscale and other mesh VPNs hand out,
// multicast, the reserved block, and the unspecified address. The
// model's input is untrusted; the machine's own interfaces and its
// neighbors are not its reading material.
//
// This is the cheap first pass, and it is not the bound. Comparing the
// literal host cannot see DNS: a name that resolves to 127.0.0.1, or
// one that answers 127.0.0.1 only on the second query, is ordinary
// text here. blockedFetchAddr at dial time is what actually decides,
// because it runs on the address the socket is about to connect to —
// which also closes 127.1, 2130706433, and 0177.0.0.1, spellings that
// no string comparison recognizes but every resolver accepts.
func checkFetchTarget(rawURL string) error {
	if localFetchAllowed() {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("web_fetch: %w", err)
	}
	// Hostnames are case-insensitive and the root dot is optional:
	// "LOCALHOST" and "localhost." are the same host as "localhost".
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "localhost" || host == "localhost.localdomain" {
		return errBlockedFetch(host)
	}
	if ip := net.ParseIP(host); ip != nil && blockedFetchIP(ip) {
		return errBlockedFetch(host)
	}
	return nil
}

func errBlockedFetch(host string) error {
	return fmt.Errorf("web_fetch: refusing to fetch %s — loopback, private, and link-local addresses are off-limits (set TILDE_ALLOW_LOCAL_FETCH=1 to allow local fetches)", host)
}

// blockedFetchIP reports whether an address is one the model may not
// reach. Everything Go's net.IP already classifies is used, plus the
// ranges it has no predicate for and that are just as much a private
// network by intent: CGNAT (100.64.0.0/10, where mesh VPNs live), the
// reserved block (240.0.0.0/4), and the broadcast address.
func blockedFetchIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127: // 100.64.0.0/10, CGNAT
			return true
		case v4[0] >= 240: // 240.0.0.0/4 reserved, and 255.255.255.255
			return true
		}
	}
	return false
}

// guardedDialContext is the http.Client's DialContext. It resolves the
// host and refuses the dial when ANY address it resolves to is one of
// ours — an all-or-nothing check, because a name with one good and one
// loopback address only has to be answered twice to land on the second.
//
// Deciding here rather than on the URL is the whole point: the decision
// is made on the address actually connected to, so DNS rebinding has no
// window to exploit and every alias spelling is covered by the same
// predicate.
func guardedDialContext() func(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: webFetchTimeout}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if localFetchAllowed() {
			return dialer.DialContext(ctx, network, addr)
		}
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("web_fetch: %w", err)
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("web_fetch: cannot resolve %s: %w", host, err)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("web_fetch: %s resolved to no addresses", host)
		}
		for _, ip := range ips {
			if blockedFetchIP(ip.IP) {
				return nil, errBlockedFetch(host + " (" + ip.IP.String() + ")")
			}
		}
		return dialer.DialContext(ctx, network, addr)
	}
}

// blockedFetchViaProxy closes the hole the dial-time guard cannot see:
// when an HTTP proxy is configured (HTTPS_PROXY, common behind a
// corporate egress), the transport dials the PROXY, so guardedDialContext
// checks the proxy's address and the TARGET is resolved by the proxy —
// in another process, where neither the dial guard nor the spelling check
// applies. A name like metadata.google.internal sails straight through to
// the cloud metadata service.
//
// So when a proxy would handle the request, the target is resolved here,
// where it can still be refused. Without a proxy this is a no-op and the
// dial guard stands alone: resolving twice would only add latency and a
// window the dial check already closes.
func blockedFetchViaProxy(req *http.Request) error {
	if localFetchAllowed() {
		return nil
	}
	proxy, err := http.ProxyFromEnvironment(req)
	if err != nil || proxy == nil {
		return nil
	}
	host := req.URL.Hostname()
	ips, err := net.DefaultResolver.LookupIPAddr(req.Context(), host)
	if err != nil {
		return fmt.Errorf("web_fetch: cannot resolve %s: %w", host, err)
	}
	for _, ip := range ips {
		if blockedFetchIP(ip.IP) {
			return errBlockedFetch(host)
		}
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
		Transport: &http.Transport{
			// The dial-time bound. A custom Transport is required to
			// install it; the default one is replaced wholesale, so
			// the proxy setting is carried over by hand.
			Proxy:               http.ProxyFromEnvironment,
			DialContext:         guardedDialContext(),
			TLSHandshakeTimeout: webFetchTimeout,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= webFetchMaxRedirs {
				return fmt.Errorf("web_fetch: stopped after %d redirects", webFetchMaxRedirs)
			}
			// Every redirect hop is re-validated: an external URL
			// bouncing to the machine's own services is the classic
			// SSRF pivot.
			if err := checkFetchTarget(req.URL.String()); err != nil {
				return err
			}
			return blockedFetchViaProxy(req)
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("web_fetch: %w", err)
	}
	req.Header.Set("User-Agent", WebFetchUA)
	if err := blockedFetchViaProxy(req); err != nil {
		return "", err
	}
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

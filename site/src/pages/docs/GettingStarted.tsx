import DocsShell from "@/components/DocsShell";
import { Terminal } from "@/components/ui/primitives";

export default function GettingStarted() {
  return (
    <DocsShell source="https://github.com/Chmgx81/opcode/blob/main/README.md">
      <h1 className="text-4xl font-medium tracking-tighter">Getting started</h1>
      <h2>Install</h2>
      <pre><code>curl -fsSL https://raw.githubusercontent.com/Chmgx81/opcode/main/install.sh | bash</code></pre>
      <p>The installer verifies a sha256 checksum before it installs anything and refuses plaintext non-loopback downloads. Prefer reading it first?</p>
      <pre><code>curl -fsSL https://raw.githubusercontent.com/Chmgx81/opcode/main/install.sh | less</code></pre>
      <p>Or build from source (Go 1.25+):</p>
      <pre><code>go install github.com/Chmgx81/opcode/cmd/opcode@latest</code></pre>
      <h2>First run</h2>
      <p>Run <code>opcode</code> in a project directory. With no configuration, the UI is the whole setup:</p>
      <div className="not-prose my-6"><Terminal title="opcode — first run">
        <div className="t-dim">welcome to opcode — setup is three steps, all in this window:</div>
        <div><span className="t-sub">  1. pick a provider below — esc closes it, /models reopens it</span></div>
        <div><span className="t-sub">  2. paste its API key when asked — masked, stored 0600 in auth.json</span></div>
        <div><span className="t-sub">  3. pick a model from the live list — the choice is remembered</span></div>
        <div className="mt-3"><span className="t-acc">❯</span> anthropic <span className="t-sub">enter to browse models</span></div>
        <div><span className="t-sub">  groq      no key — /login groq</span></div>
      </Terminal></div>
      <p>Sending with no key for the active provider is refused before the request starts, with the fix named. Nothing is billed for a doomed round.</p>
      <h2>Every day</h2>
      <p>Type a prompt, press <kbd>enter</kbd>. While a turn runs you can type to steer it (enter folds your message in at the next round boundary) or <kbd>alt</kbd>+<kbd>enter</kbd> to queue a follow-up. <kbd>esc</kbd> interrupts. <kbd>ctrl</kbd>+<kbd>c</kbd> pressed twice exits — from anywhere, including dialogs.</p>
      <p><kbd>?</kbd> opens the full shortcut sheet, <code>/</code> the command palette, and <code>/help</code> lists every command.</p>
    </DocsShell>
  );
}

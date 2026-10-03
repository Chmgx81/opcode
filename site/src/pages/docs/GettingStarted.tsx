import DocsShell from "@/components/DocsShell";
import { Figure } from "@/components/ui/primitives";
import help from "@/content/help.txt?raw";

export default function GettingStarted() {
  return (
    <DocsShell source="https://github.com/Chmgx81/opcode/blob/main/README.md">
      <h1>Getting started</h1>
      <h2>Install</h2>
      <pre><code>curl -fsSL https://raw.githubusercontent.com/Chmgx81/opcode/main/install.sh | bash</code></pre>
      <p>The installer verifies a sha256 checksum before it installs anything, and refuses plaintext downloads that are not loopback. Prefer reading it first?</p>
      <pre><code>curl -fsSL https://raw.githubusercontent.com/Chmgx81/opcode/main/install.sh | less</code></pre>
      <p>Or build from source, with Go 1.25 or newer:</p>
      <pre><code>go install github.com/Chmgx81/opcode/cmd/opcode@latest</code></pre>
      <h2>First run</h2>
      <p>Run <code>opcode</code> in a project directory. With no configuration present, the interface is the whole setup:</p>
      <ol>
        <li>Pick a provider. Each row says whether it needs a key. <kbd>esc</kbd> closes the picker, <code>/models</code> reopens it.</li>
        <li>Paste the API key when asked. It is masked on screen and stored 0600 in <code>auth.json</code>.</li>
        <li>Pick a model from the list fetched live. The choice is remembered, and <code>/login</code> and <code>/model</code> work at any time.</li>
      </ol>
      <p>Sending with no key for the active provider is refused before the request starts, with the fix named. Nothing is billed for a doomed round.</p>
      <h2>Every day</h2>
      <p>Type a prompt, press <kbd>enter</kbd>. While a turn runs you can type to steer it: enter folds your message in at the next round boundary, and <kbd>alt</kbd>+<kbd>enter</kbd> queues a follow-up. <kbd>esc</kbd> interrupts. <kbd>ctrl</kbd>+<kbd>c</kbd> pressed twice exits, from anywhere, including inside dialogs.</p>
      <p><kbd>?</kbd> opens the full shortcut sheet, <code>/</code> the command palette, and <code>/help</code> lists every command.</p>
      <h2>The command line</h2>
      <p>Everything the binary accepts on a first invocation:</p>
      <Figure fig="Fig. 1" title="opcode --help, printed by the release build." source="the v0.6.0 release build">
        {help}
      </Figure>
    </DocsShell>
  );
}

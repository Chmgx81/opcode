import { Link } from "react-router-dom";

import DocsShell from "@/components/DocsShell";

export default function Security() {
  return (
    <DocsShell source="https://github.com/Chmgx81/opcode/blob/main/SECURITY.md">
      <h1>Security</h1>
      <h2>The sandbox</h2>
      <p>Every shell command the model runs executes under a Linux kernel sandbox. <strong>Landlock</strong> confines the filesystem: reads anywhere, writes confined to the project directory, <code>/tmp</code> and language dev caches. A hand-written <strong>seccomp</strong> filter blocks network sockets on x86_64.</p>
      <p>The sandbox fails closed. If any rule cannot be applied, the command does not run. On macOS and Windows the file confinement applies, and commands are gated by prompts instead.</p>
      <h2>The permission gate</h2>
      <p>Every tool call passes a permission decision before it runs. The dialog shows the literal command, <strong>No</strong> is never preselected, and a "don't ask again" grant is canonicalized and fails closed on metacharacters, never wider than what the dialog showed.</p>
      <p>Grant rules cover flags written either way, <code>--yes</code> and <code>-y</code>, and deliberately refuse to merge pairs that mean different things in different tools: <code>--all</code> and <code>-a</code> is <code>--text</code> in grep.</p>
      <h2>Secrets</h2>
      <p>Keys live in <code>~/.opcode/auth.json</code>, mode 0600, never in a project directory. Every key in the process is registered with a redactor that scrubs it from tool output, error text, the audit log and session saves.</p>
      <p>Reads and writes of the credentials file are denied at every layer, checked on the opened descriptor by inode, so a sandboxed command cannot swap a symlink in under the check. <code>!command</code> secret-manager values resolve only from the user-level file, with a 10-second timeout, and their output is never displayed.</p>
      <h2>Untrusted text</h2>
      <p>Everything the model or a tool produced, streamed text, reasoning, tool arguments, results, subagent output and session previews, passes a display-boundary sanitizer that strips terminal escape sequences before it can reach your terminal.</p>
      <p>Model-provided instructions in project files are fenced as data in the system prompt. Project-level skills only load after you trust the project.</p>
      <h2>What opcode deliberately does not do</h2>
      <ul>
        <li>No telemetry. One optional update check, a version compare against the latest release tag, off with one environment variable.</li>
        <li>No background downloads. <code>opcode update</code> runs only when you type it, and verifies a sha256 before extracting.</li>
        <li>Release integrity is checksums, not signatures: verified in transit, trusting the GitHub account. Enable 2FA on it.</li>
        <li>MCP servers run unsandboxed by design, behind project trust, with their environment scrubbed to an allowlist.</li>
      </ul>
      <p>The full posture, including known limits, lives in <a href="https://github.com/Chmgx81/opcode/blob/main/SECURITY.md">SECURITY.md</a>. What the website and the binary each transmit is written down in the <Link className="link" to="/privacy">privacy policy</Link>.</p>
    </DocsShell>
  );
}

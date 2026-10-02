import DocsShell from "@/components/DocsShell";

export default function Headless() {
  return (
    <DocsShell source="https://github.com/Chmgx81/opcode/blob/main/README.md">
      <h1 className="text-4xl font-medium tracking-tighter">Headless &amp; CI</h1>
      <p>One turn, no UI:</p>
      <pre><code>opcode -p "explain what the Makefile's release target does"</code></pre>
      <p>Permissions fail closed with nobody to ask — for unattended automation, set the posture in <code>config.json</code> (<code>"mode": "full-auto"</code>) or scope the run to a project you trust and let the sandbox confine it.</p>
      <p><code>--json</code> emits one JSON event object per line — streamed text, tool calls and results, subagent progress, usage — for scripting:</p>
      <pre><code>opcode -p --json "list the failing packages" | jq -r 'select(.kind=="tool_result")'</code></pre>
      <p>Output is redacted against the session's secrets and sanitized at the display boundary, same as the TUI. Headless runs save the session like any other; a nil prompt fails closed rather than hanging.</p>
    </DocsShell>
  );
}

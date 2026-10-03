import DocsShell from "@/components/DocsShell";
import { Figure } from "@/components/ui/primitives";
import headless from "@/content/headless.jsonl?raw";

export default function Headless() {
  return (
    <DocsShell source="https://github.com/Chmgx81/opcode/blob/main/README.md">
      <h1>Headless and CI</h1>
      <p>One turn, no interface:</p>
      <pre><code>opcode -p "explain what the Makefile's release target does"</code></pre>
      <p>Permissions fail closed, with nobody to ask. For unattended automation either set the posture in <code>config.json</code> with <code>"mode": "full-auto"</code>, or scope the run to a project you trust and let the sandbox confine it.</p>
      <h2>Events</h2>
      <p><code>--json</code> emits one JSON event object per line: streamed text, tool calls and their results, subagent progress, and usage. Output is redacted against the session's secrets and sanitized at the display boundary, exactly as in the interface.</p>
      <pre><code>opcode -p --json "list the failing packages" | jq -r 'select(.kind=="tool_result")'</code></pre>
      <p>The run below is a full three-round turn. The second round asked for an unsandboxed command, and the refusal came back as an ordinary <code>tool_result</code> rather than an interactive prompt.</p>
      <Figure fig="Fig. 1" title="One headless turn, start to done, as opcode printed it." source="the v0.6.0 release build">
        {headless}
      </Figure>
      <h2>Also useful</h2>
      <table>
        <thead><tr><th>Flag</th><th>Effect</th></tr></thead>
        <tbody>
          <tr><td><code>--plain</code></td><td>ASCII glyphs and no animation, the screen-reader posture.</td></tr>
          <tr><td><code>--trust</code></td><td>Pre-approve the project's executable surface, for CI checkouts.</td></tr>
          <tr><td><code>--resume PATH</code></td><td>Resume one session by path.</td></tr>
          <tr><td><code>--continue</code></td><td>Resume the newest readable session.</td></tr>
        </tbody>
      </table>
      <p>Headless runs save the session like any other. A nil prompt fails closed rather than hanging.</p>
    </DocsShell>
  );
}

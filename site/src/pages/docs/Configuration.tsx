import DocsShell from "@/components/DocsShell";

const configJson = `{
  "model": "anthropic/claude-sonnet-4.5",
  "mode": "build",
  "theme": "dark",
  "context_window": 200000,
  "compaction_model": "openrouter/cheap-sum",
  "update_checks": false
}`;

const modelsJson = `{
  "default_provider": "anthropic",
  "providers": {
    "my-proxy": {
      "base_url": "https://proxy.internal/v1",
      "api": "openai",
      "api_key_env": "MY_PROXY_KEY",
      "models": ["gpt-4o", "gpt-4o-mini"]
    }
  }
}`;

export default function Configuration() {
  return (
    <DocsShell source="https://github.com/Chmgx81/opcode/blob/main/README.md">
      <h1>Configuration</h1>
      <p>Everything lives under <code>~/.opcode/</code>. You rarely touch it by hand: <code>/login</code> stores keys, <code>/model</code> persists the model, <code>/theme</code> the palette. The files are plain JSON, and they are documented here.</p>
      <h2>config.json</h2>
      <pre><code>{configJson}</code></pre>
      <ul>
        <li><code>model</code>: <code>provider/model</code>, written by picking a model in the interface.</li>
        <li><code>mode</code>: <code>plan</code>, <code>build</code>, or <code>full-auto</code>.</li>
        <li><code>theme</code>: <code>dark</code> (violet), <code>light</code>, <code>teal</code>, or <code>green</code>.</li>
        <li><code>context_window</code>: the model's context size in tokens. It enables compaction and the working line's occupancy readout. Absent or 0 means unknown, and nothing is guessed.</li>
        <li><code>compaction_model</code>: the model that summarizes older context. Unset means compaction is off.</li>
        <li><code>update_checks</code>: set <code>false</code> to disable the startup version check for good.</li>
      </ul>
      <h2>auth.json</h2>
      <p>Provider keys, written by <code>/login</code>, stored 0600. A value starting with <code>!</code> runs as a command, such as <code>op read …</code>, for a secret manager. That form resolves only from this file, never from a project directory.</p>
      <h2>models.json</h2>
      <p>Custom endpoints and pinned model lists:</p>
      <pre><code>{modelsJson}</code></pre>
      <p>An explicit entry always wins over the built-in catalog, and custom providers join every picker. A loopback <code>base_url</code> needs no key at all.</p>
      <h2>Environment</h2>
      <table>
        <tbody>
          <tr><td><code>OPCODE_HOME</code></td><td>Move the config directory. Default <code>~/.opcode</code>.</td></tr>
          <tr><td><code>OPCODE_THEME</code></td><td><code>dark</code> or <code>light</code>, for when the terminal background is undetectable.</td></tr>
          <tr><td><code>OPCODE_PLAIN</code></td><td>ASCII-only posture: no unicode glyphs, no animation. Also detected automatically for screen readers.</td></tr>
          <tr><td><code>OPCODE_NO_UPDATE_CHECK</code></td><td>Disable the startup update check.</td></tr>
          <tr><td><code>OPCODE_ALLOW_LOCAL_FETCH</code></td><td>Let <code>web_fetch</code> reach loopback and private addresses.</td></tr>
          <tr><td><code>&lt;NAME&gt;_API_KEY</code></td><td>Provider keys, when <code>auth.json</code> does not hold one.</td></tr>
        </tbody>
      </table>
    </DocsShell>
  );
}

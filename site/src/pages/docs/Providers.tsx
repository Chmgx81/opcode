import { Link } from "react-router-dom";

import DocsShell from "@/components/DocsShell";

const providers: [string, string, string][] = [
  ["openrouter (default)", "openrouter.ai/api/v1", "OPENROUTER_API_KEY"],
  ["anthropic", "Messages API, native client", "ANTHROPIC_API_KEY"],
  ["openai", "api.openai.com/v1", "OPENAI_API_KEY"],
  ["mistral", "api.mistral.ai/v1", "MISTRAL_API_KEY"],
  ["google", "Gemini, OpenAI-compatible", "GEMINI_API_KEY"],
  ["nvidia", "integrate.api.nvidia.com/v1", "NVIDIA_API_KEY"],
  ["groq · deepseek · together · cerebras · xai · moonshot · fireworks · qwen", "OpenAI-compatible", "<NAME>_API_KEY"],
  ["ollama", "localhost:11434", "none"],
];

export default function Providers() {
  return (
    <DocsShell source="https://github.com/Chmgx81/opcode/blob/main/README.md">
      <h1>Providers</h1>
      <p>Keys resolve in order: <code>auth.json</code>, written by <code>/login</code>, then the environment. Credentials never load from a project directory.</p>
      <table>
        <thead><tr><th>Provider</th><th>Endpoint</th><th>Key</th></tr></thead>
        <tbody>
          {providers.map(([p, e, k]) => (
            <tr key={p}><td><code>{p}</code></td><td>{e}</td><td><code>{k}</code></td></tr>
          ))}
        </tbody>
      </table>
      <h2>Choosing a model</h2>
      <p><code>/login</code> stores a key at any time, and <code>/login &lt;provider&gt;</code> skips the picker. Once a key exists, that provider's live model list is one enter away.</p>
      <p><code>/model</code> is the hub: models pinned in <code>models.json</code> first, then the providers you already hold keys for, whose lists browse in place, then the keyless ones, each naming the <code>/login</code> that unlocks it. Sending with no key for the active provider is refused before the request, with the fix named.</p>
      <h2>Custom endpoints and proxies</h2>
      <p>An explicit entry in <code>models.json</code>'s <code>providers</code> block always wins over the catalog. See <Link className="link" to="/docs/configuration">configuration</Link> for the shape of it. Custom providers join every picker, and their live lists fetch like any built-in.</p>
      <p>A loopback <code>base_url</code>, such as <code>localhost:11434</code> for ollama, needs no key at all. Anything else reads <code>api_key_env</code> if you named one, then the derived <code>&lt;NAME&gt;_API_KEY</code>.</p>
    </DocsShell>
  );
}

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
      <h1 className="text-4xl font-medium tracking-tighter">Providers</h1>
      <p>Keys resolve in order: <code>auth.json</code> (written by <code>/login</code>), then the environment. Credentials never load from a project directory. <code>/models</code> fetches a provider's live model list; <code>/model</code> is the hub — pinned models first, providers you hold keys for next, keyless ones one <code>/login</code> away.</p>
      <table>
        <thead><tr><th>Provider</th><th>Endpoint</th><th>Key</th></tr></thead>
        <tbody>
          {providers.map(([p, e, k]) => (
            <tr key={p}><td><code>{p}</code></td><td>{e}</td><td><code>{k}</code></td></tr>
          ))}
        </tbody>
      </table>
      <h2>Custom endpoints and proxies</h2>
      <p>An explicit entry in <code>models.json</code>'s <code>providers</code> block always wins over the catalog — see configuration. Custom providers join every picker, and their live lists are fetchable like any built-in.</p>
    </DocsShell>
  );
}

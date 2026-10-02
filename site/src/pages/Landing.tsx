import { Link } from "react-router-dom";
import { ArrowRight } from "lucide-react";

import ModernLandingHero from "@/components/ui/modern-landing-hero";
import { Reveal, SectionHeader, Chip, Terminal } from "@/components/ui/primitives";

const metrics = [
  { value: "15", label: "providers built in — switched live" },
  { value: "579", label: "tests, green under -race" },
  { value: "5", label: "platforms per release" },
  { value: "0", label: "telemetry, accounts, subscriptions" },
];

const turn = [
  { span: "read_file", file: "internal/tui/view.go", start: "0.0s", dur: "0.2s", left: 0, width: 2, active: false },
  { span: "edit_file", file: "internal/tui/view.go", start: "0.3s", dur: "1.1s", left: 2, width: 8, active: false },
  { span: "bash", file: "go test ./internal/tui/", start: "1.6s", dur: "4.2s", left: 11, width: 30, active: false },
  { span: "model", file: "streaming the answer", start: "6.1s", dur: "8.0s", left: 43, width: 57, active: true },
];

const modes = [
  { chip: "plan", tone: "ok" as const, desc: "Read-only tools; every write, command, or fetch proposes a plan you approve." },
  { chip: "build", tone: "blue" as const, desc: "Sandboxed commands and in-tree writes run free; anything else asks." },
  { chip: "full-auto", tone: "lite" as const, desc: "Everything proceeds without asking — the sandbox still confines writes." },
];

const faqs = [
  { q: "Do I need an account or a subscription?", a: "No. opcode talks to your provider with your key. There is nothing to sign up for and nothing to pay us — you pay your provider, as always." },
  { q: "Does it phone home?", a: "One update check at startup (at most daily, three-second cap, silent when offline) compares the running version against the latest release tag. OPCODE_NO_UPDATE_CHECK=1 or \"update_checks\": false turns it off. That is the only request opcode makes on its own." },
  { q: "What happens if the model tries to run something dangerous?", a: "In build mode (the default), sandboxed commands and in-tree writes run free; anything touching outside the project opens a permission dialog showing the literal command. Nothing is preselected, and grants are never wider than what the dialog showed." },
  { q: "Which platforms?", a: "Linux, macOS, and Windows binaries for every release. The kernel sandbox is Linux-only (Landlock 5.13+); elsewhere the file confinement applies and commands are gated by prompts." },
  { q: "Can I use it in CI or scripts?", a: "Yes — opcode -p \"prompt\" runs one headless turn; --json emits one JSON event per line. Use full-auto for unattended runs; permissions fail closed with nobody to ask." },
  { q: "Where do my sessions and keys live?", a: "~/.opcode/ — sessions in sessions/, keys in auth.json (0600), config in config.json. Nothing is written to a project directory except the work you asked for." },
];

const tiles = [
  "Kernel sandbox",
  "Live model lists",
  "Syntax-highlighted diffs",
  "Todo panel",
  "@ file mentions",
  "Clipboard images",
  "Shell mode (!)",
  "Markdown streaming",
  "Transcript pager",
  "Compaction",
  "Prompt history",
  "Four themes",
  "Screen-reader posture",
  "MCP servers",
  "Skills system",
  "/doctor",
];

function Card({ children, className = "" }: { children: React.ReactNode; className?: string }) {
  return (
    <div className={`rounded-xl border border-white/[0.08] bg-white/[0.02] p-6 transition-colors hover:border-white/[0.16] ${className}`}>
      {children}
    </div>
  );
}

export default function Landing() {
  return (
    <>
      <ModernLandingHero />

      {/* METRICS */}
      <section className="mx-auto max-w-6xl px-6 pt-24">
        <Reveal className="grid grid-cols-2 divide-x divide-white/[0.08] border border-white/[0.08] text-center md:grid-cols-4">
          {metrics.map((m, i) => (
            <div key={m.label} className={`p-7 ${i > 1 ? "max-md:border-t max-md:border-white/[0.08]" : ""}`}>
              <p className="font-mono text-5xl leading-none tracking-[-0.06em] text-white">{m.value}</p>
              <p className="mt-4 font-mono text-[12px] leading-relaxed text-neutral-500">{m.label}</p>
            </div>
          ))}
        </Reveal>
      </section>

      {/* EVENT STREAM */}
      <section id="turn" className="mx-auto max-w-6xl px-6 pt-24">
        <SectionHeader
          kicker="Event stream"
          title="The anatomy of a turn."
          lede="Every tool call is a span you can read — what ran, when it started, how long it took. Sandbox denials name themselves; interrupts keep the evidence."
        />
        <Reveal className="mt-10 overflow-hidden rounded-xl border border-white/[0.08]">
          <div className="flex flex-wrap items-center gap-2.5 border-b border-white/[0.08] bg-white/[0.02] px-5 py-4">
            <span className="font-mono text-[13px] text-neutral-300">
              trace <span className="text-neutral-500">turn-8f3a</span>
            </span>
            <Chip tone="ok">complete</Chip>
            <Chip>14.2s</Chip>
            <Chip tone="blue">sandbox on</Chip>
          </div>
          <div className="grid md:grid-cols-[240px_1fr]">
            <div className="flex gap-5 overflow-x-auto border-b border-white/[0.08] p-4 font-mono text-[13px] md:flex-col md:gap-3 md:border-b-0 md:border-r">
              {turn.map((t) => (
                <span key={t.span} className="flex items-center gap-2 whitespace-nowrap">
                  <span className={`h-1.5 w-1.5 rounded-full ${t.active ? "bg-[#52a8ff]" : "bg-[#62c073]"}`} />
                  <span className="text-neutral-400">{t.span}</span>
                </span>
              ))}
            </div>
            <table className="w-full font-mono text-[13px]">
              <thead>
                <tr className="text-left">
                  <th className="px-5 py-3 text-[11px] uppercase tracking-wider text-neutral-600">span</th>
                  <th className="hidden px-5 py-3 text-[11px] uppercase tracking-wider text-neutral-600 sm:table-cell">start</th>
                  <th className="px-5 py-3 text-[11px] uppercase tracking-wider text-neutral-600">duration</th>
                </tr>
              </thead>
              <tbody>
                {turn.map((t) => (
                  <tr key={t.span} className="border-t border-white/[0.06] hover:bg-white/[0.02]">
                    <td className="px-5 py-3.5">
                      <span className="text-neutral-200">{t.span}</span>
                      <span className="text-neutral-600"> {t.file}</span>
                    </td>
                    <td className="hidden px-5 py-3.5 text-neutral-500 sm:table-cell">{t.start}</td>
                    <td className="w-[45%] px-5 py-3.5">
                      <div className="flex items-center gap-3">
                        <div className="relative h-1.5 w-full">
                          <div
                            className={`absolute top-0 h-full rounded-sm ${t.active ? "bg-[#52a8ff]" : "bg-white/[0.18]"}`}
                            style={{ left: `${t.left}%`, width: `${t.width}%` }}
                          />
                        </div>
                        <span className="whitespace-nowrap text-neutral-500">{t.dur}</span>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Reveal>
      </section>

      {/* BENTO */}
      <section id="features" className="mx-auto max-w-6xl px-6 pt-24">
        <SectionHeader kicker="Features" title="Plan the work. Delegate it. Watch it land." />
        <div className="mt-10 grid gap-4 md:grid-cols-3">
          <Reveal>
            <Card className="flex h-full flex-col">
              <Chip tone="blue">plan mode</Chip>
              <h3 className="mt-4 text-lg font-medium text-white">Research first, act on approval</h3>
              <p className="mt-2 text-sm leading-relaxed text-neutral-400">
                The agent works read-only until a change is warranted; then it presents a plan and waits.{" "}
                <code className="rounded bg-white/[0.06] px-1 font-mono text-[12px] text-neutral-200">y</code> implements with every action still asking,{" "}
                <code className="rounded bg-white/[0.06] px-1 font-mono text-[12px] text-neutral-200">a</code> with auto-accept.
              </p>
              <div className="mt-6 flex flex-col gap-2.5 border-t border-white/[0.08] pt-4 font-mono text-[12px]">
                <div className="flex items-center justify-between gap-3"><span className="truncate text-neutral-400">explore internal/tui</span><Chip tone="ok">done</Chip></div>
                <div className="flex items-center justify-between gap-3"><span className="truncate text-neutral-400">draft the picker extraction</span><Chip tone="blue">active</Chip></div>
                <div className="flex items-center justify-between gap-3"><span className="truncate text-neutral-400">update the call sites</span><Chip>pending</Chip></div>
              </div>
            </Card>
          </Reveal>
          <Reveal delay={0.05}>
            <Card className="flex h-full flex-col">
              <Chip tone="ok">subagents</Chip>
              <h3 className="mt-4 text-lg font-medium text-white">A second agent, a bounded scope</h3>
              <p className="mt-2 text-sm leading-relaxed text-neutral-400">
                A focused task runs in a second agent under the same permission gate — bounded by round caps and a watchdog, inheriting cancellation, unable to recurse.
              </p>
              <div className="mt-6 flex flex-col gap-3 border-t border-white/[0.08] pt-4 font-mono text-[12px]">
                <div className="flex items-center gap-3"><span className="w-16 text-neutral-400">reviewer</span><div className="h-1.5 bg-[#52a8ff]" style={{ width: "72%" }} /><span className="ml-auto text-neutral-600">2m06s</span></div>
                <div className="flex items-center gap-3"><span className="w-16 text-neutral-400">tests</span><div className="h-1.5 bg-[#62c073]" style={{ width: "38%" }} /><span className="ml-auto text-neutral-600">48s</span></div>
                <div className="flex items-center gap-3"><span className="w-16 text-neutral-400">docs</span><div className="h-1.5 bg-white/40" style={{ width: "14%" }} /><span className="ml-auto text-neutral-600">12s</span></div>
              </div>
            </Card>
          </Reveal>
          <Reveal delay={0.1}>
            <Card className="flex h-full flex-col">
              <Chip>the diff</Chip>
              <h3 className="mt-4 text-lg font-medium text-white">See the change before it lands</h3>
              <p className="mt-2 text-sm leading-relaxed text-neutral-400">
                Writes render as syntax-highlighted diffs before they are part of the tree. Expand any result with{" "}
                <code className="rounded bg-white/[0.06] px-1 font-mono text-[12px] text-neutral-200">ctrl+r</code>.
              </p>
              <div className="mt-6 border-t border-white/[0.08] pt-4 font-mono text-[12px] leading-[2]">
                <div className="border-l-2 border-[#62c073] bg-[#62c073]/[0.06] pl-3 text-[#62c073]">+ func menuRow(selected bool, label, detail string, w int)</div>
                <div className="pl-3 text-neutral-600">  const gutter = 14</div>
                <div className="border-l-2 border-[#52a8ff] bg-[#52a8ff]/[0.06] pl-3 text-[#52a8ff]">~ return selectedStyle.Width(inner).Render(row)</div>
              </div>
            </Card>
          </Reveal>
        </div>
      </section>

      {/* ZIGZAG */}
      <section className="mx-auto max-w-6xl px-6 pt-24">
        <SectionHeader kicker="In practice" title="Nothing runs on faith." />
        <div className="mt-14 flex flex-col gap-16 md:gap-20">
          <div className="grid items-center gap-8 lg:grid-cols-2 lg:gap-14">
            <Reveal>
              <Chip tone="blue">permission gate</Chip>
              <h3 className="mt-4 text-[26px] font-medium tracking-tight text-white">The literal command, and No preselected.</h3>
              <p className="mt-3 max-w-md text-[15px] leading-relaxed text-neutral-400">
                An action outside the mode's bounds opens a dialog that shows exactly what will run. The dangerous answer is never the default, and grants are canonicalized — never wider than what the dialog showed.
              </p>
            </Reveal>
            <Reveal>
              <Terminal title="opcode — approval">
                <div><span className="t-bold">Bash command</span> <span className="t-sub">· Runs a command</span></div>
                <div className="t-body mt-2">npm init -y</div>
                <div className="t-sub">opcode needs your approval to run this.</div>
                <div className="mt-2">  1. Yes</div>
                <div><span className="t-sub">  2. Yes, and don't ask again for: </span><span className="t-body">npm init:*</span></div>
                <div><span className="t-acc">❯ 3. No</span></div>
                <div className="t-dim mt-2">1-3 or arrows to choose · enter selects · esc</div>
              </Terminal>
            </Reveal>
          </div>

          <div className="grid items-center gap-8 lg:grid-cols-2 lg:gap-14">
            <Reveal className="lg:order-2">
              <Chip tone="ok">sessions</Chip>
              <h3 className="mt-4 text-[26px] font-medium tracking-tight text-white">A crash loses one turn, not the conversation.</h3>
              <p className="mt-3 max-w-md text-[15px] leading-relaxed text-neutral-400">
                The session file rewrites atomically at every turn boundary. Resume the newest with{" "}
                <code className="rounded bg-white/[0.06] px-1 font-mono text-[13px] text-neutral-200">--continue</code>{" "}
                — a damaged newest file is skipped and named — or browse with{" "}
                <code className="rounded bg-white/[0.06] px-1 font-mono text-[13px] text-neutral-200">/sessions</code>.
              </p>
            </Reveal>
            <Reveal className="lg:order-1">
              <Terminal title="opcode — sessions">
                <div><span className="t-acc">◈</span> <span className="t-body">opcode — session saved · resume it with /sessions</span></div>
                <div className="mt-2"><span className="t-dim">$</span> <span className="t-body">opcode --continue</span></div>
                <div><span className="t-ok">✓</span> <span className="t-sub">resumed session 20260102-150405 (42 messages)</span></div>
                <div className="mt-2 t-sub">20260101-182211  add rate limiting to the login route</div>
                <div className="t-sub">20260101-111040  the picker extraction, phase 46</div>
              </Terminal>
            </Reveal>
          </div>

          <div className="grid items-center gap-8 lg:grid-cols-2 lg:gap-14">
            <Reveal>
              <Chip>headless</Chip>
              <h3 className="mt-4 text-[26px] font-medium tracking-tight text-white">One flag for CI and scripts.</h3>
              <p className="mt-3 max-w-md text-[15px] leading-relaxed text-neutral-400">
                <code className="rounded bg-white/[0.06] px-1 font-mono text-[13px] text-neutral-200">-p</code> runs one turn with no UI;{" "}
                <code className="rounded bg-white/[0.06] px-1 font-mono text-[13px] text-neutral-200">--json</code>{" "}
                emits one event per line for jq. Permissions fail closed with nobody to ask.
              </p>
            </Reveal>
            <Reveal>
              <Terminal title="opcode — headless">
                <div><span className="t-dim">$</span> <span className="t-body">opcode -p --json "list the failing packages" | jq -r '.kind'</span></div>
                <div className="mt-2 t-sub">tool_result</div>
                <div className="t-sub">usage</div>
                <div className="t-ok">turn_complete</div>
              </Terminal>
            </Reveal>
          </div>
        </div>
      </section>

      {/* TILES */}
      <section className="mx-auto max-w-6xl px-6 pt-24">
        <SectionHeader kicker="And the rest" title="Small things, deliberately built." />
        <div className="mt-10 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
          {tiles.map((t, i) => (
            <Reveal key={t} delay={(i % 4) * 0.04}>
              <Card className="flex items-center gap-3 !p-4">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true">
                  <path d="M5 12h14M12 5v14" stroke={i % 4 === 0 ? "#52a8ff" : "#ededed"} strokeWidth="1.5" strokeLinecap="square" />
                </svg>
                <span className="text-[13px] leading-tight text-neutral-300">{t}</span>
              </Card>
            </Reveal>
          ))}
        </div>
      </section>

      {/* MODES */}
      <section className="mx-auto max-w-6xl px-6 pt-24">
        <SectionHeader kicker="Three modes" title="Cycle it with Tab, or pick it." />
        <div className="mt-10 grid gap-4 md:grid-cols-3">
          {modes.map((m, i) => (
            <Reveal key={m.chip} delay={i * 0.05}>
              <Card>
                <Chip tone={m.tone}>{m.chip}</Chip>
                <p className="mt-4 text-sm leading-relaxed text-neutral-400">{m.desc}</p>
              </Card>
            </Reveal>
          ))}
        </div>
      </section>

      {/* PROVIDERS */}
      <section className="mx-auto max-w-6xl px-6 pt-24">
        <SectionHeader kicker="Providers" title="Fifteen built in, custom endpoints welcome." />
        <Reveal className="mt-10 overflow-hidden rounded-xl border border-white/[0.08]">
          <table className="w-full font-mono text-[13px]">
            <tbody>
              {[
                ["openrouter (default)", "openrouter.ai/api/v1", "OPENROUTER_API_KEY"],
                ["anthropic", "Messages API, native client", "ANTHROPIC_API_KEY"],
                ["mistral · google · groq · …", "OpenAI-compatible", "<NAME>_API_KEY"],
                ["ollama", "localhost:11434", "none"],
              ].map(([p, e, k]) => (
                <tr key={p} className="border-t border-white/[0.06] first:border-t-0 hover:bg-white/[0.02]">
                  <td className="px-5 py-3.5 text-[#52a8ff]">{p}</td>
                  <td className="px-5 py-3.5 text-neutral-400">{e}</td>
                  <td className="hidden px-5 py-3.5 text-neutral-500 sm:table-cell">{k}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Reveal>
        <Reveal className="mt-5 text-sm font-mono text-neutral-500">
          keys resolve from auth.json (with !command for secret managers), then the environment — never from a project directory.{" "}
          <Link className="text-[#52a8ff] hover:underline" to="/docs/providers">full table →</Link>
        </Reveal>
      </section>

      {/* FAQ */}
      <section className="mx-auto max-w-6xl px-6 pt-24">
        <SectionHeader kicker="FAQ" title="Questions people actually ask." />
        <div className="mt-8 grid max-w-3xl gap-3">
          {faqs.map((f, i) => (
            <Reveal key={f.q} delay={i * 0.03}>
              <details className="rounded-xl border border-white/[0.08] bg-white/[0.02]">
                <summary className="flex cursor-pointer list-none items-center justify-between gap-4 px-5 py-4 font-medium text-white">
                  {f.q}
                  <span className="text-[#52a8ff]">+</span>
                </summary>
                <p className="max-w-[62ch] px-5 pb-5 text-sm leading-relaxed text-neutral-400">{f.a}</p>
              </details>
            </Reveal>
          ))}
        </div>
      </section>

      <section className="mx-auto max-w-6xl px-6 pb-8 pt-24">
        <Reveal>
          <Link
            to="/docs"
            className="inline-flex h-11 items-center gap-2 rounded-md bg-white px-6 text-sm font-medium text-black transition-all hover:bg-neutral-200 active:scale-[0.98]"
          >
            Read the docs <ArrowRight className="h-4 w-4" />
          </Link>
        </Reveal>
      </section>
    </>
  );
}

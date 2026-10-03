import { Link } from "react-router-dom";
import { useState, useEffect } from "react";

import { cn } from "@/lib/utils";
import {
  Figure,
  Install,
  Ruled,
  Section,
  Release,
  Badge,
  StatCard,
  ComparisonTable,
  FeatureCard
} from "@/components/ui/primitives";

import approval from "@/content/approval.txt?raw";
import diff from "@/content/diff.txt?raw";
import firstRun from "@/content/first-run.txt?raw";
import headless from "@/content/headless.jsonl?raw";

const INSTALL = "curl -fsSL https://raw.githubusercontent.com/Chmgx81/opcode/main/install.sh | bash";

// Enhanced facts with icons
const facts = [
  { value: "15", label: "providers built in, switched live", icon: "🔗" },
  { value: "579", label: "tests, green under -race", icon: "✅" },
  { value: "5", label: "platforms in every release", icon: "🖥️" },
  { value: "0", label: "telemetry, accounts, subscriptions", icon: "🔒" },
];

// Core capabilities with better descriptions
const capabilities: { term: string; description: string; icon: string }[] = [
  {
    term: "Kernel Sandbox",
    description: "Shell commands run under a Landlock ruleset on Linux: reads anywhere, writes confined to the project, /tmp and dev caches, network sockets blocked. The kernel enforces it, not opcode.",
    icon: "🛡️"
  },
  {
    term: "Multi-Provider",
    description: "Fifteen built in, switched live with /model. Keys are held in auth.json at 0600 or read from the environment, and never loaded out of a project directory.",
    icon: "🌐"
  },
  {
    term: "Permission Modes",
    description: "plan, build and full-auto, cycled with tab. Each is a different answer to one question: what is allowed to run without asking.",
    icon: "🔐"
  },
  {
    term: "Crash-Resistant",
    description: "The session file rewrites atomically at every turn boundary, so a crash costs you the turn in flight and nothing else. --continue picks up the newest.",
    icon: "💾"
  },
  {
    term: "Subagents",
    description: "A second agent with a bounded scope, round caps and a watchdog, running under the same permission gate and unable to recurse.",
    icon: "🤖"
  },
  {
    term: "Headless Mode",
    description: "-p runs a single turn with no UI. --json writes one event object per line. When there is nobody to answer a prompt, permissions fail closed.",
    icon: "⚡"
  },
];

// Safety features
const safety: { term: string; description: string; icon: string }[] = [
  {
    term: "Landlock",
    description: "Every command runs under a hand-written ruleset. If any rule cannot be applied, the command does not run.",
    icon: "🛡️"
  },
  {
    term: "Seccomp Filter",
    description: "On x86_64 a filter blocks network sockets inside the sandbox, so a spawned process cannot phone home.",
    icon: "🚫"
  },
  {
    term: "Permission Gate",
    description: "The approval dialog shows the literal command, preselects nothing, and canonicalises every always-allow so a grant can never grow wider than what you saw.",
    icon: "🔐"
  },
  {
    term: "Secret Redaction",
    description: "Keys are registered with a redactor that scrubs them from tool output, errors, the audit log and saved sessions.",
    icon: "🔒"
  },
];

// Providers table
const providers: [string, string, string][] = [
  ["openrouter", "openrouter.ai/api/v1", "OPENROUTER_API_KEY"],
  ["anthropic", "Messages API, native client", "ANTHROPIC_API_KEY"],
  ["openai", "api.openai.com/v1", "OPENAI_API_KEY"],
  ["mistral", "api.mistral.ai/v1", "MISTRAL_API_KEY"],
  ["google", "Gemini, OpenAI-compatible", "GEMINI_API_KEY"],
  ["nvidia", "integrate.api.nvidia.com/v1", "NVIDIA_API_KEY"],
  ["groq, deepseek, together, cerebras, xai, moonshot, fireworks, qwen", "OpenAI-compatible", "<NAME>_API_KEY"],
  ["ollama", "localhost:11434", "none"],
];

// Enhanced FAQs
const faqs: [string, string][] = [
  [
    "Do I need an account or a subscription?",
    "No. opcode talks to your provider with your key. There is nothing to sign up for and nothing to pay us: you pay your provider, as you always have.",
  ],
  [
    "Does it phone home?",
    "One update check at startup, at most daily, with a three-second cap and silent when offline, compares the running version against the latest release tag. OPCODE_NO_UPDATE_CHECK=1, or \"update_checks\": false, turns it off. That is the only request opcode makes on its own.",
  ],
  [
    "What happens when the model tries to run something dangerous?",
    "In build mode, the default, sandboxed commands and in-tree writes run freely. Anything touching outside the project opens a permission dialog showing the literal command, with nothing preselected. A grant is never wider than what the dialog showed.",
  ],
  [
    "Which platforms?",
    "Linux, macOS and Windows binaries for every release. The kernel sandbox is Linux-only, on Landlock 5.13 and newer. Elsewhere the file confinement still applies and commands are gated by prompts.",
  ],
  [
    "Can I use it in CI or scripts?",
    "Yes. opcode -p \"prompt\" runs one headless turn, and --json emits one JSON event per line. Use full-auto for unattended runs, because permissions fail closed when there is nobody to ask.",
  ],
  [
    "Where do my sessions and keys live?",
    "In ~/.opcode/: sessions in sessions/, keys in auth.json at 0600, configuration in config.json. Nothing is written into a project directory except the work you asked for.",
  ],
];

// Competitor comparison data
const competitorFeatures = [
  "Kernel Sandbox",
  "Multi-Provider Support",
  "Headless Mode",
  "Session Persistence",
  "Subagents",
  "Open Source",
  "No Telemetry",
  "Permission Modes"
];

const competitorsData = [
  {
    name: "opcode",
    values: [true, true, true, true, true, true, true, true]
  },
  {
    name: "Claude Code",
    values: [false, false, false, true, false, false, true, false]
  },
  {
    name: "Cursor",
    values: [false, true, false, true, false, false, true, false]
  },
  {
    name: "Cline",
    values: [false, true, true, true, false, true, true, false]
  },
  {
    name: "Codex",
    values: [false, false, false, true, false, false, true, false]
  }
];

// Key differentiators
const differentiators = [
  {
    title: "True Isolation",
    description: "Kernel-enforced sandboxing with Landlock and seccomp. Not just a policy - the OS enforces it.",
    icon: "🛡️"
  },
  {
    title: "Provider Freedom",
    description: "15 built-in providers, switch live at runtime. Bring your own endpoint. Your keys, your control.",
    icon: "🌐"
  },
  {
    title: "Permission Philosophy",
    description: "Three modes, clear boundaries. plan/build/full-auto. You decide what runs without asking.",
    icon: "🔐"
  },
  {
    title: "Zero Overhead",
    description: "One binary, no daemon, no telemetry. Just you, your terminal, and the work to be done.",
    icon: "⚡"
  }
];

export default function Landing() {
  const [isScrolled, setIsScrolled] = useState(false);

  useEffect(() => {
    const handleScroll = () => {
      setIsScrolled(window.scrollY > 50);
    };

    window.addEventListener('scroll', handleScroll);
    return () => window.removeEventListener('scroll', handleScroll);
  }, []);

  return (
    <>
      {/* ENHANCED HERO SECTION */}
      <section className="relative border-b border-rule bg-gradient-to-br from-paper via-paper-2 to-paper">
        <div className="absolute inset-0 bg-grid-pattern opacity-50 pointer-events-none"></div>
        <div className="mx-auto max-w-6xl px-6 pt-16 pb-20 md:pt-24 md:pb-28 relative">
          {/* Badge row */}
          <div className="flex flex-wrap items-center gap-4 justify-center md:justify-start mb-8">
            <Badge variant="secondary">
              Terminal coding agent
            </Badge>
            <Badge variant="secondary">
              One Go binary
            </Badge>
            <Badge variant="secondary">
              MIT
            </Badge>
          </div>

          <div className="grid gap-x-12 gap-y-8 lg:grid-cols-[minmax(0,1.25fr)_minmax(0,0.75fr)] items-start">
            <div>
              <h1 className="text-[clamp(2.5rem,6.2vw,4.75rem)] font-semibold leading-[1.02] tracking-[-0.03em] text-balance">
                Reads the repo.
                <br />
                Writes the code.
                <br />
                <span className="text-ink-3">Asks before it acts.</span>
              </h1>
              
              {/* Enhanced value proposition */}
              <p className="mt-8 max-w-[52ch] text-[1.125rem] leading-[1.65] text-ink-2">
                <strong className="text-ink">opcode</strong> is a terminal coding agent that puts safety first. 
                With kernel-enforced sandboxing, three permission modes, and support for 15+ providers, 
                it gives you the power of AI coding assistance without compromising security.
              </p>

              {/* Enhanced CTA section */}
              <div className="mt-10 flex flex-col gap-4 sm:flex-row sm:items-center sm:gap-6">
                <Install cmd={INSTALL} className="flex-1" />
                <div className="flex flex-wrap items-center gap-4">
                  <Link
                    to="/docs"
                    className="btn btn-secondary flex items-center gap-2"
                  >
                    <span>Read docs</span>
                    <span aria-hidden="true">→</span>
                  </Link>
                  <a
                    href="https://github.com/Chmgx81/opcode"
                    className="label text-ink-2 hover:text-accent transition-colors duration-200 underline decoration-accent decoration-1 underline-offset-4"
                  >
                    Star on GitHub
                  </a>
                </div>
              </div>

              {/* Platform badges */}
              <p className="mono mt-8 text-[0.8125rem] text-ink-3 flex flex-wrap items-center gap-x-4 gap-y-2">
                <span className="flex items-center gap-2">
                  <span>🐧</span> Linux
                </span>
                <span className="text-rule-2">|</span>
                <span className="flex items-center gap-2">
                  <span>🍎</span> macOS
                </span>
                <span className="text-rule-2">|</span>
                <span className="flex items-center gap-2">
                  <span>🪟</span> Windows
                </span>
                <span className="text-rule-2 mx-2">|</span>
                <span>No account</span>
                <span className="text-rule-2">|</span>
                <span>No telemetry</span>
                <span className="text-rule-2">|</span>
                <span>MIT licensed</span>
              </p>
            </div>

            {/* Hero terminal showcase */}
            <div className="hidden lg:block">
              <div className="sticky top-32">
                <div className="bg-paper-2 border border-rule p-6 shadow-lg">
                  <div className="flex items-center gap-2 mb-4">
                    <div className="w-3 h-3 bg-accent rounded-full"></div>
                    <div className="w-3 h-3 bg-accent-2 rounded-full"></div>
                    <div className="w-3 h-3 bg-accent-3 rounded-full"></div>
                  </div>
                  <pre className="capture text-[0.75rem]">
{`opcode

  Welcome to opcode v0.6.0
  Type your prompt and press Enter
  
  Press / for commands, ? for help
  
  [build mode] █
`}
                  </pre>
                </div>
              </div>
            </div>
          </div>

          {/* Hero terminal capture */}
          <Figure
            fig="Fig. 1"
            title="A real session asking permission before a command leaves the project. Nothing is preselected, and the default answer is No."
            source="the v0.6.0 release build"
          >
            {approval}
          </Figure>
        </div>
      </section>

      {/* ENHANCED FACTS SECTION */}
      <section className="border-b border-rule bg-paper-2">
        <div className="mx-auto max-w-6xl px-6">
          <div className="grid grid-cols-2 md:grid-cols-4">
            {facts.map((f, i) => (
              <StatCard
                key={f.label}
                value={f.value}
                label={f.label}
                icon={f.icon}
                className={cn(
                  i % 2 === 0 ? "pr-6" : "border-l border-rule pl-6",
                  i >= 2 && "border-t border-rule md:border-t-0",
                  i === 2 && "md:border-l md:border-rule md:pl-6",
                )}
              />
            ))}
          </div>
        </div>
      </section>

      {/* COMPETITOR COMPARISON SECTION */}
      <Section
        id="comparison"
        n="00"
        title="How opcode stacks up."
        lede="Not all coding agents are created equal. Here's how opcode compares to popular alternatives."
        tone="recessed"
      >
        <div className="overflow-x-auto">
          <ComparisonTable 
            features={competitorFeatures} 
            competitors={competitorsData}
          />
        </div>
        <p className="mt-6 max-w-[64ch] text-[1.0625rem] leading-relaxed text-ink-2">
          opcode stands out with its kernel-level sandboxing, true multi-provider support, 
          and clear permission boundaries. No other agent offers this combination of 
          <strong className="text-ink">security</strong>, <strong className="text-ink">flexibility</strong>, and <strong className="text-ink">transparency</strong>.
        </p>
      </Section>

      {/* KEY DIFFERENTIATORS SECTION */}
      <Section
        id="differentiators"
        n="00-A"
        title="What sets opcode apart."
        lede="Four pillars that make opcode the most secure and flexible coding agent available."
      >
        <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-4">
          {differentiators.map((d, i) => (
            <FeatureCard
              key={d.title}
              icon={d.icon}
              title={d.title}
              description={d.description}
              className="h-full"
              delay={i * 100}
            />
          ))}
        </div>
      </Section>

      {/* 01 CAPABILITIES */}
      <Section
        id="capabilities"
        n="01"
        title="What is in the binary."
        lede="Six things it does today. Each one is in the source, and each claim on this page is checked against it."
      >
        <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
          {capabilities.map((cap, i) => (
            <div 
              key={cap.term} 
              className="card p-6 transition-all duration-300 hover:shadow-md"
              style={{ animationDelay: `${i * 50}ms` }}
            >
              <div className="text-2xl mb-4">{cap.icon}</div>
              <h3 className="text-lg font-semibold text-ink mb-3">{cap.term}</h3>
              <p className="text-ink-2 leading-relaxed text-[0.9375rem]">{cap.description}</p>
            </div>
          ))}
        </div>
      </Section>

      {/* 02 FIRST RUN */}
      <Section
        n="02"
        title="No config file to write."
        lede="Run opcode in a project directory. The interface is the setup: pick a provider, paste its key, pick a model from the list it fetches live."
        tone="recessed"
      >
        <p className="max-w-[62ch] text-[1.0625rem] leading-relaxed text-ink-2">
          The key is masked as you type, stored 0600 in auth.json, and the provider and model are
          remembered for next launch. Sending with no key for the active provider is refused before
          the request goes out, so a doomed round is never billed.
        </p>
        <Figure
          fig="Fig. 2"
          title="First run in an empty config directory. Three steps, all inside the window."
          source="the v0.6.0 release build"
        >
          {firstRun}
        </Figure>
      </Section>

      {/* 03 MODES */}
      <Section
        id="modes"
        n="03"
        title="Three modes, cycled with tab."
        lede="The mode is the only thing that decides what may run without asking."
      >
        <div className="card mb-8">
          <Ruled
            head={["Mode", "What runs without asking"]}
            rows={[
              ["plan", "Read-only tools. Every write, command or fetch becomes a plan you approve."],
              ["build (default)", "Sandboxed commands and in-tree writes. Anything else asks."],
              [
                "full-auto",
                "Everything, with the sandbox still confining writes to the project, /tmp and dev caches.",
              ],
            ]}
            hoverable
          />
        </div>
        <p className="max-w-[62ch] text-[1.0625rem] leading-relaxed text-ink-2">
          When something asks, the dialog shows the literal command and offers a scoped
          always-allow. Prefix rules fail closed on shell metacharacters, and grants are
          canonicalised so they can never be broader than the dialog you approved.
        </p>
      </Section>

      {/* 04 SAFETY */}
      <Section
        id="safety"
        n="04"
        title="A sandbox you can read."
        lede="Enforced below the process, by the kernel, and closed by default when something cannot be enforced."
        tone="ink"
      >
        <div className="grid gap-6 md:grid-cols-2">
          {safety.map((s, i) => (
            <div 
              key={s.term} 
              className="card border-t-0 border-band-rule p-6"
            >
              <div className="text-2xl mb-4 text-band-accent">{s.icon}</div>
              <dt className="label text-band-dim">{s.term}</dt>
              <dd className="mt-2 text-[0.9375rem] leading-relaxed text-band-mid">{s.description}</dd>
            </div>
          ))}
        </div>
        <p className="mt-8 max-w-[62ch] text-[1.0625rem] leading-relaxed text-band-mid">
          Where the kernel cannot confine, opcode asks instead of pretending. The threat model, and
          the limits of it, are written down in{" "}
          <a
            href="https://github.com/Chmgx81/opcode/blob/main/SECURITY.md"
            className="link text-band-fg"
          >
            SECURITY.md
          </a>
          .
        </p>
      </Section>

      {/* 05 DIFF */}
      <Section
        id="diff"
        n="05"
        title="The change, before it lands."
        lede="Writes render as a diff before they are part of the tree. Nothing arrives as a surprise."
      >
        <p className="max-w-[62ch] text-[1.0625rem] leading-relaxed text-ink-2">
          Expand any result with <kbd>ctrl</kbd>+<kbd>r</kbd>. <kbd>ctrl</kbd>+<kbd>o</kbd> opens
          the transcript, the whole conversation scrollable, which is where the frame below was
          taken from.
        </p>
        <Figure
          fig="Fig. 3"
          title="An edit applied in build mode, opened in the transcript pager. The diff is the record."
          source="the v0.6.0 release build"
        >
          {diff}
        </Figure>
      </Section>

      {/* 06 PROVIDERS */}
      <Section
        id="providers"
        n="06"
        title="Fifteen providers, and any endpoint you point at."
        lede="Switch live at runtime with /model. The built-in catalog is only a default: an explicit entry in models.json always wins."
        tone="recessed"
      >
        <div className="card">
          <Ruled
            head={["Provider", "Endpoint", "Key"]}
            rows={providers.map(([p, e, k]) => [
              <code key={p}>{p}</code>,
              e,
              <code className="text-ink-3">{k}</code>,
            ])}
            hoverable
          />
        </div>
        <p className="mt-6 max-w-[64ch] text-[1.0625rem] leading-relaxed text-ink-2">
          Keys resolve from auth.json, which supports <code>!command</code> for a secret manager,
          then the environment.{" "}
          <Link to="/docs/providers" className="link">
            The full table is in the documentation
          </Link>
          .
        </p>
      </Section>

      {/* 07 HEADLESS */}
      <Section
        id="headless"
        n="07"
        title="One flag for CI."
        lede="opcode -p runs a single turn with no interface. --json writes one event object per line, ready for jq."
      >
        <p className="max-w-[62ch] text-[1.0625rem] leading-relaxed text-ink-2">
          With nobody present to answer a prompt, permissions fail closed. In the run below the
          model asked for an unsandboxed command and was refused before anything executed, which is
          the behaviour you want in a pipeline.
        </p>
        <Figure fig="Fig. 4" title="The event stream of one headless turn, printed by opcode." source="the v0.6.0 release build">
          {headless}
        </Figure>
        <p className="mt-6 text-[1.0625rem] leading-relaxed text-ink-2">
          <Link to="/docs/headless" className="link">
            Headless and CI reference
          </Link>
          .
        </p>
      </Section>

      {/* FAQ */}
      <Section n="08" title="Questions people ask." lede="Answered from the code, not from a pitch.">
        <div className="max-w-[70ch]">
          {faqs.map(([q, a]) => (
            <details 
              key={q} 
              className="group border-b border-rule transition-colors duration-200 hover:bg-paper-2"
            >
              <summary className="flex cursor-pointer list-none items-baseline justify-between gap-6 py-4 text-[1.0625rem] font-semibold text-ink hover:text-accent transition-colors duration-200">
                <span>{q}</span>
                <span className="mono shrink-0 text-[1.1rem] leading-none text-accent" aria-hidden="true">
                  <span className="group-open:hidden">+</span>
                  <span className="hidden group-open:inline">−</span>
                </span>
              </summary>
              <p className="max-w-[64ch] pb-5 text-[1rem] leading-relaxed text-ink-2">{a}</p>
            </details>
          ))}
        </div>
      </Section>

      {/* FINAL CTA SECTION */}
      <section className="border-t border-ink relative">
        <div className="absolute inset-0 bg-gradient-to-tr from-accent via-accent-2 to-accent-3 opacity-5 pointer-events-none"></div>
        <div className="mx-auto max-w-6xl px-6 py-16 md:py-24 relative">
          <div className="grid gap-x-12 gap-y-8 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
            <div>
              <p className="label text-accent">Ready to code securely</p>
              <h2 className="mt-4 max-w-[20ch] text-[clamp(1.75rem,3.2vw,2.6rem)] font-semibold leading-[1.08] tracking-[-0.02em]">
                Try it in a real repository.
              </h2>
              <p className="mt-6 max-w-[52ch] text-[1rem] leading-relaxed text-ink-2">
                Join developers who trust opcode for safe, efficient coding assistance. 
                Install in seconds, code with confidence.
              </p>
            </div>
            <div>
              <div className="space-y-4">
                <Install cmd={INSTALL} />
                <p className="text-[0.875rem] text-ink-3">
                  The installer verifies a sha256 checksum before it installs anything. 
                  Prefer to read it first? Pipe it into <code>less</code>.
                </p>
                <div className="flex flex-wrap items-center gap-4">
                  <Link to="/docs" className="btn btn-primary">
                    Read the documentation
                  </Link>
                  <a 
                    href="https://github.com/Chmgx81/opcode" 
                    className="btn btn-secondary"
                  >
                    View on GitHub
                  </a>
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>
    </>
  );
}

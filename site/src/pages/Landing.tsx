import { Link } from "react-router-dom";

import { cn } from "@/lib/utils";
import { Figure, Install, Ruled, Section } from "@/components/ui/primitives";
import approval from "@/content/approval.txt?raw";
import diff from "@/content/diff.txt?raw";
import firstRun from "@/content/first-run.txt?raw";
import headless from "@/content/headless.jsonl?raw";

const INSTALL = "curl -fsSL https://raw.githubusercontent.com/Chmgx81/opcode/main/install.sh | bash";

const facts = [
  { value: "15", label: "providers built in, switched live" },
  { value: "579", label: "tests, green under -race" },
  { value: "5", label: "platforms in every release" },
  { value: "0", label: "telemetry, accounts, subscriptions" },
];

const capabilities: [string, string][] = [
  [
    "sandbox",
    "Shell commands run under a Landlock ruleset on Linux: reads anywhere, writes confined to the project, /tmp and dev caches, network sockets blocked. The kernel enforces it, not opcode.",
  ],
  [
    "providers",
    "Fifteen built in, switched live with /model. Keys are held in auth.json at 0600 or read from the environment, and never loaded out of a project directory.",
  ],
  [
    "modes",
    "plan, build and full-auto, cycled with tab. Each is a different answer to one question: what is allowed to run without asking.",
  ],
  [
    "sessions",
    "The session file rewrites atomically at every turn boundary, so a crash costs you the turn in flight and nothing else. --continue picks up the newest.",
  ],
  [
    "subagents",
    "A second agent with a bounded scope, round caps and a watchdog, running under the same permission gate and unable to recurse.",
  ],
  [
    "headless",
    "-p runs a single turn with no UI. --json writes one event object per line. When there is nobody to answer a prompt, permissions fail closed.",
  ],
];

const safety: [string, string][] = [
  [
    "landlock",
    "Every command runs under a hand-written ruleset. If any rule cannot be applied, the command does not run.",
  ],
  [
    "seccomp",
    "On x86_64 a filter blocks network sockets inside the sandbox, so a spawned process cannot phone home.",
  ],
  [
    "gate",
    "The approval dialog shows the literal command, preselects nothing, and canonicalises every always-allow so a grant can never grow wider than what you saw.",
  ],
  [
    "secrets",
    "Keys are registered with a redactor that scrubs them from tool output, errors, the audit log and saved sessions.",
  ],
];

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

function Spec({ term, children }: { term: string; children: React.ReactNode }) {
  return (
    <div className="border-t border-rule pt-4">
      <dt className="label text-accent">{term}</dt>
      <dd className="mt-2 text-[0.9375rem] leading-relaxed text-ink-2">{children}</dd>
    </div>
  );
}

export default function Landing() {
  return (
    <>
      {/* LEAD */}
      <section className="border-b border-rule">
        <div className="mx-auto max-w-6xl px-6 pt-14 pb-16 md:pt-20 md:pb-20">
          <p className="label text-accent">Terminal coding agent · one Go binary · MIT</p>

          <div className="mt-6 grid gap-x-12 gap-y-6 lg:grid-cols-[minmax(0,1.25fr)_minmax(0,0.75fr)]">
            <h1 className="text-[clamp(2.5rem,6.2vw,4.75rem)] font-semibold leading-[1.02] tracking-[-0.03em] text-balance">
              Reads the repo.
              <br />
              Writes the code.
              <br />
              <span className="text-ink-3">Asks before it acts.</span>
            </h1>
            <div className="lg:pt-3">
              <p className="max-w-[46ch] text-[1.125rem] leading-[1.65] text-ink-2">
                opcode is a coding agent that lives in your terminal. Fifteen providers, a kernel
                sandbox, three permission modes, subagents, and sessions that survive a crash.
              </p>
              <p className="mt-4 max-w-[46ch] text-[1.125rem] leading-[1.65] text-ink-2">
                One binary, on your machine, with your key.
              </p>
            </div>
          </div>

          <div className="mt-10 flex flex-col gap-5 sm:flex-row sm:items-center sm:gap-8">
            <Install cmd={INSTALL} />
            <div className="flex shrink-0 flex-wrap items-center gap-x-6 gap-y-3">
              <Link
                to="/docs"
                className="label inline-flex items-center bg-ink px-5 py-3 text-paper hover:bg-accent"
              >
                Read the documentation
              </Link>
              <a
                href="https://github.com/Chmgx81/opcode"
                className="label text-ink-2 underline decoration-accent decoration-1 underline-offset-4 hover:text-accent"
              >
                Source on GitHub
              </a>
            </div>
          </div>

          <p className="mono mt-6 text-[0.8125rem] text-ink-3">
            linux, macos, windows
            <span className="mx-2 text-rule-2">/</span>
            no account, no telemetry
            <span className="mx-2 text-rule-2">/</span>
            MIT licensed
          </p>

          <Figure
            fig="Fig. 1"
            title="A real session asking permission before a command leaves the project. Nothing is preselected, and the default answer is No."
            source="the v0.6.0 release build"
          >
            {approval}
          </Figure>
        </div>
      </section>

      {/* FACTS */}
      <section className="border-b border-rule bg-paper-2">
        <div className="mx-auto max-w-6xl px-6">
          <dl className="grid grid-cols-2 md:grid-cols-4">
            {facts.map((f, i) => (
              <div
                key={f.label}
                className={cn(
                  "py-7 md:py-8",
                  i % 2 === 0 ? "pr-6" : "border-l border-rule pl-6",
                  i >= 2 && "border-t border-rule md:border-t-0",
                  i === 2 && "md:border-l md:border-rule md:pl-6",
                )}
              >
                <dt className="mono text-[clamp(1.75rem,3.5vw,2.5rem)] leading-none tracking-[-0.04em] text-ink">
                  {f.value}
                </dt>
                <dd className="mt-3 max-w-[22ch] text-[0.8125rem] leading-snug text-ink-3">{f.label}</dd>
              </div>
            ))}
          </dl>
        </div>
      </section>

      {/* 01 CAPABILITIES */}
      <Section
        id="capabilities"
        n="01"
        title="What is in the binary."
        lede="Six things it does today. Each one is in the source, and each claim on this page is checked against it."
      >
        <dl className="grid gap-x-12 gap-y-7 sm:grid-cols-2">
          {capabilities.map(([term, body]) => (
            <Spec key={term} term={term}>
              {body}
            </Spec>
          ))}
        </dl>
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
        />
        <p className="mt-6 max-w-[62ch] text-[1.0625rem] leading-relaxed text-ink-2">
          When something asks, the dialog shows the literal command and offers a scoped
          always-allow. Prefix rules fail closed on shell metacharacters, and grants are
          canonicalised so they can never be broader than the dialog you approved.
        </p>
      </Section>

      {/* 04 SAFETY, on ink */}
      <Section
        id="safety"
        n="04"
        title="A sandbox you can read."
        lede="Enforced below the process, by the kernel, and closed by default when something cannot be enforced."
        tone="ink"
      >
        <dl className="grid gap-x-12 gap-y-7 sm:grid-cols-2">
          {safety.map(([term, body]) => (
            <div key={term} className="border-t border-band-rule pt-4">
              <dt className="label text-band-dim">{term}</dt>
              <dd className="mt-2 text-[0.9375rem] leading-relaxed text-band-mid">{body}</dd>
            </div>
          ))}
        </dl>
        <p className="mt-8 max-w-[62ch] text-[1.0625rem] leading-relaxed text-band-mid">
          Where the kernel cannot confine, opcode asks instead of pretending. The threat model, and
          the limits of it, are written down in{" "}
          <a
            href="https://github.com/Chmgx81/opcode/blob/main/SECURITY.md"
            className="text-band-fg underline decoration-band-accent decoration-1 underline-offset-4 hover:text-band-accent"
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
        <Ruled
          head={["Provider", "Endpoint", "Key"]}
          rows={providers.map(([p, e, k]) => [
            p,
            e,
            <span className="text-ink-3">{k}</span>,
          ])}
        />
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
        <div className="max-w-[70ch] border-t border-rule">
          {faqs.map(([q, a]) => (
            <details key={q} className="group border-b border-rule">
              <summary className="flex cursor-pointer list-none items-baseline justify-between gap-6 py-4 text-[1.0625rem] font-semibold text-ink hover:text-accent">
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

      {/* CLOSE */}
      <section className="border-t border-ink">
        <div className="mx-auto max-w-6xl px-6 py-16 md:py-24">
          <div className="grid gap-x-12 gap-y-8 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
            <div>
              <p className="label text-accent">Install</p>
              <h2 className="mt-4 max-w-[16ch] text-[clamp(1.75rem,3.2vw,2.6rem)] font-semibold leading-[1.08] tracking-[-0.02em]">
                Try it in a real repository.
              </h2>
            </div>
            <div>
              <Install cmd={INSTALL} />
              <p className="mt-5 max-w-[52ch] text-[1rem] leading-relaxed text-ink-2">
                The installer verifies a sha256 checksum before it installs anything. Prefer to read
                it first? Pipe it into <code>less</code>. Or build from source with Go 1.25 or newer.
              </p>
              <p className="mt-5">
                <Link to="/docs" className="link">
                  Read the documentation
                </Link>
              </p>
            </div>
          </div>
        </div>
      </section>
    </>
  );
}

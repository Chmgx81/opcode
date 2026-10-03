import { Link } from "react-router-dom";
import {
  ArrowRight,
  Check,
  Clock,
  Cpu,
  GitBranch,
  Layers,
  Lock,
  RefreshCw,
  Repeat,
  Server,
  Shield,
  Terminal as TerminalIcon,
  Users,
  Zap,
} from "react-feather";

import {
  Feature,
  Figure,
  Install,
  QA,
  Release,
  Reveal,
  Ruled,
  Section,
  Terminal,
} from "@/components/ui/primitives";
import approval from "@/content/approval.txt?raw";
import diff from "@/content/diff.txt?raw";
import firstRun from "@/content/first-run.txt?raw";
import headless from "@/content/headless.jsonl?raw";
import help from "@/content/help.txt?raw";

const INSTALL = "curl -fsSL https://raw.githubusercontent.com/Chmgx81/opcode/main/install.sh | bash";
const RELEASE_NOTE = "the v0.6.0 release build";

const stats = [
  { value: "15", label: "providers built in, switched live" },
  { value: "579", label: "tests, green under -race" },
  { value: "5", label: "platforms in every release" },
  { value: "0", label: "telemetry, accounts, subscriptions" },
];

const platforms = [
  { icon: <TerminalIcon size={16} />, label: "linux" },
  { icon: <Server size={16} />, label: "macos" },
  { icon: <Cpu size={16} />, label: "windows" },
  { icon: <Layers size={16} />, label: "x86_64 & arm64" },
  { icon: <Lock size={16} />, label: "landlock sandbox" },
];

/** The six capabilities, as the icon + title + body triple every reference uses. */
const capabilities = [
  {
    icon: <Shield size={18} />,
    term: "sandbox",
    title: "A kernel-enforced sandbox",
    body: "Shell commands run under a Landlock ruleset on Linux: reads anywhere, writes confined to the project, /tmp and dev caches, network sockets blocked. The kernel enforces it, not opcode.",
  },
  {
    icon: <Zap size={18} />,
    term: "providers",
    title: "Fifteen providers, live",
    body: "Switched at runtime with /model. Keys are held in auth.json at 0600 or read from the environment, and never loaded out of a project directory.",
  },
  {
    icon: <Repeat size={18} />,
    term: "modes",
    title: "Three permission modes",
    body: "plan, build and full-auto, cycled with tab. Each is a different answer to one question: what is allowed to run without asking.",
  },
  {
    icon: <RefreshCw size={18} />,
    term: "sessions",
    title: "Sessions that survive a crash",
    body: "The session file rewrites atomically at every turn boundary, so a crash costs you the turn in flight and nothing else. --continue picks up the newest.",
  },
  {
    icon: <Users size={18} />,
    term: "subagents",
    title: "Bounded subagents",
    body: "A second agent with a bounded scope, round caps and a watchdog, running under the same permission gate and unable to recurse.",
  },
  {
    icon: <TerminalIcon size={18} />,
    term: "headless",
    title: "One flag for CI",
    body: "-p runs a single turn with no UI. --json writes one event object per line. When there is nobody to answer a prompt, permissions fail closed.",
  },
];

/** The enforcement stack. A panel rather than a figure: there is no honest
 *  screenshot of a seccomp filter, and stating the layers is also the point. */
const enforcement: [string, string][] = [
  ["landlock", "Every command runs under a hand-written ruleset. If any rule cannot be applied, the command does not run."],
  ["seccomp", "On x86_64 a filter blocks network sockets inside the sandbox, so a spawned process cannot phone home."],
  ["gate", "The approval dialog shows the literal command, preselects nothing, and canonicalises every always-allow so a grant can never grow wider than what you saw."],
  ["secrets", "Keys are registered with a redactor that scrubs them from tool output, errors, the audit log and saved sessions."],
];

const providers: [string, string, string][] = [
  ["openrouter", "openrouter.ai/api/v1", "OPENROUTER_API_KEY"],
  ["anthropic", "Messages API, native client", "ANTHROPIC_API_KEY"],
  ["openai", "api.openai.com/v1", "OPENAI_API_KEY"],
  ["mistral", "api.mistral.ai/v1", "MISTRAL_API_KEY"],
  ["google", "Gemini, OpenAI-compatible", "GEMINI_API_KEY"],
  ["nvidia", "integrate.api.nvidia.com/v1", "NVIDIA_API_KEY"],
  ["groq · deepseek · together · cerebras · xai · moonshot · fireworks · qwen", "OpenAI-compatible", "<NAME>_API_KEY"],
  ["ollama", "localhost:11434", "none"],
];

const faqs: [string, string][] = [
  ["Do I need an account or a subscription?", "No. opcode talks to your provider with your key. There is nothing to sign up for and nothing to pay us: you pay your provider, as you always have."],
  ["Does it phone home?", "One update check at startup, at most daily, with a three-second cap and silent when offline, compares the running version against the latest release tag. OPCODE_NO_UPDATE_CHECK=1, or \"update_checks\": false, turns it off. That is the only request opcode makes on its own."],
  ["What happens when the model tries to run something dangerous?", "In build mode, the default, sandboxed commands and in-tree writes run freely. Anything touching outside the project opens a permission dialog showing the literal command, with nothing preselected. A grant is never wider than what the dialog showed."],
  ["Which platforms?", "Linux, macOS and Windows binaries for every release. The kernel sandbox is Linux-only, on Landlock 5.13 and newer. Elsewhere the file confinement still applies and commands are gated by prompts."],
  ["Can I use it in CI or scripts?", "Yes. opcode -p \"prompt\" runs one headless turn, and --json emits one JSON event per line. Use full-auto for unattended runs, because permissions fail closed when there is nobody to ask."],
  ["Where do my sessions and keys live?", "In ~/.opcode/: sessions in sessions/, keys in auth.json at 0600, configuration in config.json. Nothing is written into a project directory except the work you asked for."],
];

function Checks({ items }: { items: string[] }) {
  return (
    <ul className="checks mt-5">
      {items.map((t) => (
        <li key={t}>
          <Check size={15} strokeWidth={2.5} aria-hidden="true" />
          <span className="text-[0.9375rem] leading-relaxed text-ink-2">{t}</span>
        </li>
      ))}
    </ul>
  );
}

export default function Landing() {
  return (
    <>
      {/* ---------------------------------------------------------------- HERO */}
      <section className="glow border-b border-rule">
        <div className="lattice pointer-events-none absolute inset-0 -z-10" aria-hidden="true" />
        <div className="shell pt-16 pb-16 md:pt-24 md:pb-24">
          <div className="flex justify-center">
            <p className="chip chip--accent rise" style={{ "--d": 0 } as never}>
              <span className="pulse" aria-hidden="true" />
              <Release /> · one Go binary · MIT
            </p>
          </div>

          <h1
            className="h-display rise mx-auto mt-8 max-w-[16ch] text-center text-[clamp(2.75rem,7.6vw,5.25rem)]"
            style={{ "--d": 1 } as never}
          >
            Reads the repo. Writes the code.{" "}
            <span className="text-accent">Asks before it acts.</span>
          </h1>

          <p
            className="lede rise mx-auto mt-7 max-w-[54ch] text-center"
            style={{ "--d": 2 } as never}
          >
            A coding agent that lives in your terminal. Fifteen providers, a kernel sandbox, three
            permission modes, subagents, and sessions that survive a crash. One binary, on your
            machine, with your key.
          </p>

          <div className="rise mx-auto mt-9 flex max-w-3xl flex-col gap-3" style={{ "--d": 3 } as never}>
            <Install cmd={INSTALL} />
            <div className="flex flex-wrap items-center justify-center gap-3">
              <Link to="/docs" className="btn btn--fill">
                Read the documentation
              </Link>
              <a href="https://github.com/Chmgx81/opcode" className="btn btn--edge">
                Source on GitHub
              </a>
            </div>
          </div>

          <div className="rise mt-14 md:mt-20" style={{ "--d": 4 } as never}>
            <Figure
              hero
              live
              fig="Fig. 1"
              title="A real session asking permission before a command leaves the project. Nothing is preselected, and the default answer is No."
              source={RELEASE_NOTE}
            >
              {approval}
            </Figure>
          </div>
        </div>
      </section>

      {/* -------------------------------------------------------- PLATFORMS */}
      <section className="border-b border-rule">
        <div className="shell py-10">
          <ul className="flex flex-wrap items-center justify-center gap-x-10 gap-y-5">
            {platforms.map((p, i) => (
              <Reveal
                as="li"
                key={p.label}
                delay={i}
                className="flex items-center gap-2.5 text-ink-3"
              >
                <span className="text-ink-3" aria-hidden="true">
                  {p.icon}
                </span>
                <span className="label">{p.label}</span>
              </Reveal>
            ))}
          </ul>
        </div>
      </section>

      {/* ------------------------------------------------------------- STATS */}
      <section className="border-b border-rule bg-paper-2">
        <div className="shell">
          <dl className="grid grid-cols-2 md:grid-cols-4">
            {stats.map((s, i) => (
              <Reveal
                key={s.label}
                delay={i}
                className={[
                  "py-9 md:py-11",
                  i % 2 === 1 ? "border-l border-rule pl-6 md:pl-8" : "pr-6 md:pr-8",
                  i >= 2 ? "border-t border-rule md:border-t-0" : "",
                  i === 2 ? "md:border-l md:border-rule" : "",
                ].join(" ")}
              >
                <dt className="mono text-[clamp(1.875rem,3.6vw,2.5rem)] font-medium leading-none tracking-[-0.03em] text-ink">
                  {s.value}
                </dt>
                <dd className="mt-3 max-w-[22ch] text-[0.8125rem] leading-snug text-ink-3">
                  {s.label}
                </dd>
              </Reveal>
            ))}
          </dl>
        </div>
      </section>

      {/* ------------------------------------------------- WHAT IS IN IT */}
      <Section
        eyebrow="Built in"
        title="What is in the binary."
        lede="Six things it does today. Each one is in the source, and each claim on this page is checked against it."
      >
        <div className="grid gap-x-10 gap-y-12 sm:grid-cols-2 lg:grid-cols-3">
          {capabilities.map((c, i) => (
            <Reveal key={c.term} delay={i % 3}>
              <Feature icon={c.icon} title={c.title} term={c.term}>
                {c.body}
              </Feature>
            </Reveal>
          ))}
        </div>

        <div className="mt-16">
          <Figure
            fig="Fig. 2"
            title="The whole command surface the binary accepts."
            source={RELEASE_NOTE}
          >
            {help}
          </Figure>
        </div>
      </Section>

      {/* ------------------------------------------------------- FIRST RUN */}
      <Section
        id="first-run"
        tone="raised"
        eyebrow="Setup"
        title="No config file to write."
        lede="Run opcode in a project directory and the interface is the setup: pick a provider, paste its key, pick a model from the list it fetches live."
      >
        <Reveal className="max-w-[62ch]">
          <Checks
            items={[
              "The key is masked as you type and stored 0600 in auth.json.",
              "The provider and model are remembered for the next launch.",
              "Sending with no key is refused before the request goes out, so a doomed round is never billed.",
              "/login and /model work at any time, without a restart.",
            ]}
          />
        </Reveal>
        <Reveal delay={1} className="mt-12">
          <Figure
            fig="Fig. 3"
            title="First run in an empty config directory. Three steps, all inside the window."
            source={RELEASE_NOTE}
          >
            {firstRun}
          </Figure>
        </Reveal>
      </Section>

      {/* ---------------------------------------------------------- SAFETY */}
      <Section
        id="safety"
        eyebrow="Safety"
        title="A sandbox you can read."
        lede="Enforced below the process, by the kernel, and closed by default when something cannot be enforced."
      >
        <div className="grid gap-x-14 gap-y-10 lg:grid-cols-2">
          <Reveal className="card p-7">
            <p className="label text-accent">Four layers, in order</p>
            <dl className="mt-6">
              {enforcement.map(([term, body], i) => (
                <div
                  key={term}
                  className={[
                    "py-5",
                    i === 0 ? "pt-0" : "border-t border-rule",
                  ].join(" ")}
                >
                  <dt className="mono text-[0.8125rem] font-medium text-ink">{term}</dt>
                  <dd className="mt-1.5 text-[0.9375rem] leading-relaxed text-ink-2">{body}</dd>
                </div>
              ))}
            </dl>
          </Reveal>

          <Reveal delay={1} className="flex flex-col gap-6 lg:self-center">
            <p className="body-copy">
              The dialog shows the literal command and preselects nothing. Every always-allow is
              canonicalised, so a grant can never grow wider than the thing you actually saw. Prefix
              rules fail closed on shell metacharacters.
            </p>
            <p className="body-copy">
              Where the kernel cannot confine, opcode asks instead of pretending. The threat model,
              and the honest limits of it, are written down in{" "}
              <a
                href="https://github.com/Chmgx81/opcode/blob/main/SECURITY.md"
                className="link"
              >
                SECURITY.md
              </a>
              .
            </p>
            <div>
              <Link to="/docs/security" className="more">
                Read the security model <ArrowRight size={15} aria-hidden="true" />
              </Link>
            </div>
          </Reveal>
        </div>
      </Section>

      {/* ------------------------------------------------------------ DIFF */}
      <Section
        id="diff"
        tone="raised"
        eyebrow="Review"
        title="The change, before it lands."
        lede="Writes render as a diff before they are part of the tree, so nothing arrives as a surprise."
      >
        <Reveal className="max-w-[62ch]">
          <Checks
            items={[
              "Expand any result with ctrl+r.",
              "ctrl+o opens the transcript, the whole conversation scrollable.",
              "The diff is the record, not a summary of one.",
              "The session file rewrites atomically at every turn boundary.",
            ]}
          />
        </Reveal>
        <Reveal delay={1} className="mt-12">
          <Figure
            fig="Fig. 4"
            title="An edit applied in build mode, opened in the transcript pager."
            source={RELEASE_NOTE}
          >
            {diff}
          </Figure>
        </Reveal>
      </Section>

      {/* ----------------------------------------------------------- MODES */}
      <Section
        id="modes"
        eyebrow="Permissions"
        title="Three modes, cycled with tab."
        lede="The mode is the only thing that decides what may run without asking."
      >
        <div className="grid gap-5 lg:grid-cols-3">
          {[
            {
              id: "plan",
              title: "Read-only tools",
              body: "Every write, command or fetch becomes a plan you approve before it happens.",
              asks: true,
            },
            {
              id: "build",
              title: "Sandboxed, plus in-tree writes",
              body: "The default. Anything touching outside the project opens a permission dialog.",
              asks: true,
            },
            {
              id: "full-auto",
              title: "Everything",
              body: "With the sandbox still confining writes to the project, /tmp and dev caches.",
              asks: false,
            },
          ].map((m, i) => (
            <Reveal key={m.id} delay={i}>
              <article className="card card--hover flex h-full flex-col p-7">
                <div className="flex items-baseline justify-between gap-4">
                  <p className="mono text-[0.9375rem] font-medium text-ink">{m.id}</p>
                  <span
                    className={`label ${m.asks ? "text-accent" : "text-ink-3"}`}
                  >
                    {m.asks ? "asks first" : "runs"}
                  </span>
                </div>
                <h3 className="h-card mt-5 text-[1.0625rem] text-ink">{m.title}</h3>
                <p className="mt-2.5 text-[0.9375rem] leading-relaxed text-ink-2">{m.body}</p>
              </article>
            </Reveal>
          ))}
        </div>

        <div className="mt-14">
          <Ruled
            head={["Mode", "What runs without asking"]}
            rows={[
              ["plan", "Read-only tools. Every write, command or fetch becomes a plan you approve."],
              ["build (default)", "Sandboxed commands and in-tree writes. Anything else asks."],
              ["full-auto", "Everything, with the sandbox still confining writes to the project, /tmp and dev caches."],
            ]}
          />
        </div>
      </Section>

      {/* ------------------------------------------------------- PROVIDERS */}
      <Section
        id="providers"
        tone="raised"
        eyebrow="Providers"
        title="Fifteen providers, and any endpoint you point at."
        lede="Switch live at runtime with /model. The built-in catalog is only a default: an explicit entry in models.json always wins."
      >
        <Ruled
          head={["Provider", "Endpoint", "Key"]}
          rows={providers.map(([p, e, k]) => [p, e, <span className="text-ink-3">{k}</span>])}
        />
        <p className="body-copy mt-10 max-w-[62ch]">
          Keys resolve from auth.json, which supports <code>!command</code> for a secret manager,
          then the environment.{" "}
          <Link to="/docs/providers" className="link">
            The full table is in the documentation
          </Link>
          .
        </p>
      </Section>

      {/* -------------------------------------------------------- HEADLESS */}
      <Section
        id="headless"
        eyebrow="Automation"
        title="One flag for CI."
        lede="opcode -p runs a single turn with no interface. --json writes one event object per line, ready for jq."
      >
        <Reveal className="flex flex-col gap-6 md:flex-row md:items-start md:justify-between md:gap-16">
          <p className="body-copy max-w-[58ch]">
            With nobody present to answer a prompt, permissions fail closed. In the run below the
            model asked for an unsandboxed command and was refused before anything executed, which
            is the behaviour you want in a pipeline.
          </p>
          <Link to="/docs/headless" className="more shrink-0">
            Headless and CI reference <ArrowRight size={15} aria-hidden="true" />
          </Link>
        </Reveal>
        <Reveal delay={1} className="mt-12">
          <Figure
            fig="Fig. 5"
            title="The event stream of one headless turn, printed by opcode."
            source={RELEASE_NOTE}
          >
            {headless}
          </Figure>
        </Reveal>
      </Section>

      {/* -------------------------------------------------------------- FAQ */}
      <Section id="faq" tone="raised" eyebrow="FAQ" title="Questions people ask.">
        <div className="max-w-[68ch]">
          {faqs.map(([q, a], i) => (
            <Reveal key={q} delay={Math.min(i, 4)}>
              <QA q={q} a={a} />
            </Reveal>
          ))}
        </div>
      </Section>

      {/* ------------------------------------------------------- CTA CLOSE */}
      <section className="glow border-t border-rule">
        <div className="lattice pointer-events-none absolute inset-0 -z-10" aria-hidden="true" />
        <div className="shell py-20 md:py-28">
          {/* max-w-4xl, not 3xl: the card's own padding leaves 672px at 3xl
              and the command needs 756px. 4xl leaves 800px. */}
          <Reveal className="card mx-auto flex max-w-4xl flex-col items-center gap-7 p-8 text-center md:p-12">
            <p className="label text-accent">Install</p>
            <h2 className="h-section max-w-[18ch] text-[clamp(1.75rem,3.4vw,2.5rem)] text-ink">
              Try it in a real repository.
            </h2>
            <p className="body-copy max-w-[46ch]">
              The installer verifies a sha256 checksum before it installs anything. Prefer to read
              it first? Pipe it into <code>less</code>.
            </p>
            {/* Full width inside the card: the command needs 756px and no
                two-column split of this card can give it that. */}
            <div className="w-full min-w-0">
              <Install cmd={INSTALL} />
            </div>
            <div className="flex flex-wrap justify-center gap-3">
              <Link to="/docs" className="btn btn--fill">
                Read the documentation
              </Link>
              <a href="https://github.com/Chmgx81/opcode" className="btn btn--edge">
                Source on GitHub
              </a>
            </div>
          </Reveal>
        </div>
      </section>
    </>
  );
}

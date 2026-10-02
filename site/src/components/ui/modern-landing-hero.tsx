import { ArrowRight, ChevronRight, Search } from "lucide-react";
import { Link } from "react-router-dom";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";

// The site's hero, on the modern-landing-hero pattern: pure black
// floor, a hairline nav, a Linear-style pill badge, Vercel-style
// tracking-tighter headline, sharp white CTAs — and the mock window
// showing opcode's own first-run output, with the real "/"-to-open
// command palette (opcode opens it with "/", not ⌘K).
export function ModernLandingHero() {
  return (
    <section className="relative flex min-h-screen w-full flex-col items-center bg-black font-sans text-white selection:bg-white selection:text-black">
      <div className="absolute top-0 w-full border-b border-white/[0.08] h-16 bg-black" />

      <main className="flex w-full max-w-[1000px] flex-col items-center px-6 pt-32 text-center md:pt-40 z-10">
        {/* Pill badge */}
        <Link
          to="/docs"
          className="group mb-8 flex cursor-pointer items-center gap-2 rounded-full border border-white/[0.12] bg-white/[0.03] py-1.5 pl-1.5 pr-3 text-xs font-medium text-neutral-400 backdrop-blur-md transition-colors hover:bg-white/[0.06]"
        >
          <span className="rounded-full bg-white px-2 py-0.5 text-[10px] font-bold uppercase tracking-widest text-black">
            v0.6.0
          </span>
          <span>The scrollback release is out</span>
          <ChevronRight className="h-3.5 w-3.5 text-neutral-500 transition-transform group-hover:translate-x-0.5" />
        </Link>

        {/* Headline */}
        <h1 className="mb-6 max-w-4xl text-balance text-5xl font-medium tracking-tighter text-white sm:text-7xl lg:text-8xl">
          Ship code.
          <br className="hidden sm:block" />
          <span className="text-neutral-600">Stay in control.</span>
        </h1>

        <p className="mx-auto mb-10 max-w-[600px] text-balance text-base leading-relaxed text-neutral-400 sm:text-lg">
          A terminal coding agent in one Go binary — under your
          permission system, not around it. Any provider, your key, no
          subscription.
        </p>

        {/* CTAs */}
        <div className="flex w-full flex-col items-center justify-center gap-4 sm:flex-row">
          <Button asChild className="h-11 w-full gap-2 px-6 sm:w-auto">
            <a href="https://github.com/Chmgx81/opcode/releases">
              Get the release
              <ArrowRight className="h-4 w-4" />
            </a>
          </Button>
          <Button
            asChild
            variant="outline"
            className="h-11 w-full border-white/[0.12] px-6 sm:w-auto"
          >
            <Link to="/docs">Documentation</Link>
          </Button>
        </div>

        {/* Mock window: palette bar over the first-run terminal */}
        <div className="mt-20 w-full max-w-4xl rounded-t-xl border border-white/[0.12] bg-[#050505] shadow-2xl overflow-hidden">
          <div className="flex items-center gap-4 border-b border-white/[0.08] bg-white/[0.02] px-4 py-3">
            <div className="flex gap-1.5">
              <div className="h-2.5 w-2.5 rounded-full bg-white/[0.15]" />
              <div className="h-2.5 w-2.5 rounded-full bg-white/[0.15]" />
              <div className="h-2.5 w-2.5 rounded-full bg-white/[0.15]" />
            </div>

            <div className="mx-auto flex h-7 w-full max-w-md items-center gap-2 rounded-md border border-white/[0.08] bg-black/50 px-2.5 text-xs text-neutral-500">
              <Search className="h-3.5 w-3.5" />
              <span>type / for commands, @ for files…</span>
              <div className="ml-auto flex items-center gap-1 opacity-60">
                <span className="rounded border border-white/15 px-1">/</span>
              </div>
            </div>
          </div>

          <div className="h-72 w-full bg-[#050505] p-6 text-left font-mono text-sm leading-relaxed text-neutral-400">
            <div className="flex items-center gap-2">
              <span className="text-white">~</span>
              <span>opcode</span>
            </div>
            <div className="mt-4 text-neutral-600">
              <p>welcome to opcode — setup is three steps, all in this window:</p>
              <p>1. pick a provider below — each row says whether it needs a key</p>
              <p>2. paste its API key when asked — masked, stored 0600 in auth.json</p>
              <p>3. pick a model from the live list — the choice is remembered</p>
            </div>
            <div className="mt-4">
              <p>
                <span className="t-acc">❯</span> anthropic{" "}
                <span className="text-neutral-600">enter to browse models</span>
              </p>
              <p className="text-neutral-600">  groq      no key — /login groq</p>
              <p className="text-neutral-600">  ollama    enter to browse models</p>
            </div>
            <div className="mt-4 flex items-center gap-2">
              <div className="h-2 w-2 rounded-full bg-white animate-pulse" />
              <span className="t-ok text-white/70">key stored — fetching models…</span>
            </div>
          </div>
        </div>
      </main>
    </section>
  );
}

export default ModernLandingHero;

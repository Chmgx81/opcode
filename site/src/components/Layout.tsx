import { useEffect, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { ArrowRight } from "lucide-react";

import { cn } from "@/lib/utils";

const links = [
  { to: "/#turn", label: "How it works" },
  { to: "/#features", label: "Features" },
  { to: "/docs", label: "Docs" },
  { href: "https://github.com/Chmgx81/opcode", label: "GitHub" },
];

function Logo() {
  return (
    <Link to="/" className="flex items-center gap-2 text-lg font-medium text-white">
      <svg width="20" height="20" viewBox="0 0 64 64" aria-hidden="true">
        <path d="M18 16 L28 32 L18 48" stroke="#52a8ff" strokeWidth="6" strokeLinecap="square" fill="none" />
        <path d="M34 32 H46" stroke="#ededed" strokeWidth="6" strokeLinecap="square" />
      </svg>
      opcode
    </Link>
  );
}

export default function Layout({ children }: { children: React.ReactNode }) {
  const [open, setOpen] = useState(false);
  const { pathname } = useLocation();
  useEffect(() => setOpen(false), [pathname]);

  return (
    <div className="min-h-screen bg-black font-sans text-white">
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 focus:z-50 focus:bg-white focus:px-4 focus:py-2 focus:font-semibold focus:text-black"
      >
        Skip to content
      </a>

      <header className="sticky top-0 z-40 border-b border-white/[0.08] bg-black/80 backdrop-blur-md">
        <nav className="mx-auto flex h-16 max-w-6xl items-center gap-8 px-6">
          <Logo />
          <div className="mx-auto hidden items-center gap-8 text-sm text-neutral-400 md:flex">
            {links.map((l) =>
              l.to ? (
                <Link key={l.label} to={l.to} className="transition-colors hover:text-white">
                  {l.label}
                </Link>
              ) : (
                <a key={l.label} href={l.href} className="transition-colors hover:text-white">
                  {l.label}
                </a>
              ),
            )}
          </div>
          <Link
            to="/docs"
            className="ml-auto hidden items-center gap-2 rounded-md bg-white px-3 py-1.5 text-sm font-medium text-black transition-all hover:bg-neutral-200 active:scale-[0.98] md:inline-flex"
          >
            Docs <ArrowRight className="h-3.5 w-3.5" />
          </Link>
          <button
            className="btn ml-1 rounded-md border border-white/[0.12] px-3 py-1.5 text-sm md:hidden"
            onClick={() => setOpen(!open)}
            aria-expanded={open}
            aria-label="Menu"
          >
            <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true">
              <path d="M1 3h14M1 8h14M1 13h14" stroke="currentColor" strokeWidth="1.5" />
            </svg>
          </button>
        </nav>
        {open && (
          <div className="border-t border-white/[0.08] bg-black px-6 py-6 md:hidden">
            <div className="flex flex-col gap-4 text-lg">
              {links.map((l) =>
                l.to ? (
                  <Link key={l.label} to={l.to} className="text-neutral-300">
                    {l.label}
                  </Link>
                ) : (
                  <a key={l.label} href={l.href} className="text-neutral-300">
                    {l.label}
                  </a>
                ),
              )}
              <Link to="/docs" className="mt-2 inline-flex items-center gap-2 text-white">
                Docs <ArrowRight className="h-4 w-4" />
              </Link>
            </div>
          </div>
        )}
      </header>

      <main id="main">{children}</main>

      <footer className="mt-24 border-t border-white/[0.08]">
        <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-8 gap-y-3 px-6 py-10 font-mono text-[13px] text-neutral-600">
          <span className="flex items-center gap-2 font-sans font-medium text-white">
            <svg width="14" height="14" viewBox="0 0 64 64" aria-hidden="true">
              <path d="M18 16 L28 32 L18 48" stroke="#52a8ff" strokeWidth="6" strokeLinecap="square" fill="none" />
              <path d="M34 32 H46" stroke="#ededed" strokeWidth="6" strokeLinecap="square" />
            </svg>
            opcode
          </span>
          <span>MIT license</span>
          <a className="hover:text-[#52a8ff]" href="https://github.com/Chmgx81/opcode">github</a>
          <a className="hover:text-[#52a8ff]" href="/opcode/docs/security">security</a>
          <a className="hover:text-[#52a8ff]" href="/opcode/docs">docs</a>
          <span className={cn("ml-auto uppercase tracking-wider")}>
            no cookies · no tracking · self-hosted fonts
          </span>
        </div>
      </footer>
    </div>
  );
}

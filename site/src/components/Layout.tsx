import { useEffect, useState } from "react";
import { Link, useLocation } from "react-router-dom";

import { Release } from "@/components/ui/primitives";
import { useTheme, type Theme } from "@/lib/theme";

const nav = [
  { to: "/", label: "Overview" },
  { to: "/docs", label: "Documentation" },
  { to: "/docs/security", label: "Security" },
  { href: "https://github.com/Chmgx81/opcode", label: "GitHub" },
];

/** The supplied wordmark, drawn in the colour of whatever holds it. */
export function Wordmark({ height = 20, className }: { height?: number; className?: string }) {
  const rects: [number, number, number][] = [
    [1, 1, 4], [9, 1, 5],
    [0, 2, 2], [4, 2, 2], [8, 2, 2], [12, 2, 2],
    [0, 3, 1], [5, 3, 1], [8, 3, 1], [13, 3, 1],
    [0, 4, 1], [5, 4, 1], [8, 4, 1], [12, 4, 1],
    [0, 5, 2], [4, 5, 2], [8, 5, 4],
    [1, 6, 1], [4, 6, 1], [8, 6, 1],
    [2, 7, 2], [8, 7, 1],
  ];
  return (
    <svg
      viewBox="0 0 14 8"
      height={height}
      width={(height * 14) / 8}
      role="img"
      aria-label="opcode"
      className={className}
      shapeRendering="crispEdges"
    >
      {rects.map(([x, y, w]) => (
        <rect key={`${x}-${y}`} x={x} y={y} width={w} height={1} fill="currentColor" />
      ))}
    </svg>
  );
}

/** The theme control. It names the mode it will switch to, the way the menu
 *  button names the action it performs. */
export function ThemeToggle({ theme, onToggle }: { theme: Theme; onToggle: () => void }) {
  const to = theme === "dark" ? "light" : "dark";
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-label={`Switch to ${to} mode`}
      title={`Switch to ${to} mode`}
      className="label border border-ink px-3 py-1.5 text-ink hover:bg-accent hover:text-paper"
    >
      {to}
    </button>
  );
}

export default function Layout({ children }: { children: React.ReactNode }) {
  const [open, setOpen] = useState(false);
  const { theme, toggle } = useTheme();
  const { pathname } = useLocation();
  useEffect(() => setOpen(false), [pathname]);

  return (
    <div className="min-h-screen bg-paper font-serif text-ink">
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 focus:z-50 focus:bg-ink focus:px-4 focus:py-2 focus:font-mono focus:text-sm focus:text-paper"
      >
        Skip to content
      </a>

      <header className="sticky top-0 z-40 border-b border-rule bg-paper">
        <nav className="mx-auto flex h-14 max-w-6xl items-center gap-6 px-6">
          <Link to="/" className="shrink-0 text-ink hover:text-accent" aria-label="opcode, home">
            <Wordmark height={20} />
          </Link>
          <div className="ml-auto flex items-center gap-3 md:gap-5">
            <div className="hidden items-center gap-7 md:flex">
              {nav.map((l) =>
                l.to ? (
                  <Link key={l.label} to={l.to} className="label text-ink-2 hover:text-accent">
                    {l.label}
                  </Link>
                ) : (
                  <a key={l.label} href={l.href} className="label text-ink-2 hover:text-accent">
                    {l.label}
                  </a>
                ),
              )}
              <span className="mono border-l border-rule pl-5 text-[0.75rem] text-ink-3">
                release <span className="text-ink"><Release /></span>
              </span>
            </div>
            <ThemeToggle theme={theme} onToggle={toggle} />
            <button
              type="button"
              className="label border border-ink px-3 py-1.5 text-ink md:hidden"
              onClick={() => setOpen(!open)}
              aria-expanded={open}
            >
              {open ? "close" : "menu"}
            </button>
          </div>
        </nav>
        {open && (
          <div className="border-t border-rule bg-paper px-6 py-4 md:hidden">
            <ul className="flex flex-col">
              {nav.map((l) => (
                <li key={l.label} className="border-b border-rule last:border-b-0">
                  {l.to ? (
                    <Link to={l.to} className="label block py-3 text-ink-2">
                      {l.label}
                    </Link>
                  ) : (
                    <a href={l.href} className="label block py-3 text-ink-2">
                      {l.label}
                    </a>
                  )}
                </li>
              ))}
            </ul>
          </div>
        )}
      </header>

      <main id="main">{children}</main>

      <footer className="border-t border-band bg-band text-band-fg">
        <div className="mx-auto max-w-6xl px-6 py-14">
          <div className="grid gap-10 md:grid-cols-[minmax(0,1.2fr)_minmax(0,2fr)]">
            <div>
              <Wordmark height={24} />
              <p className="mt-4 max-w-[36ch] text-[0.9375rem] leading-relaxed text-band-mid">
                A terminal coding agent in one Go binary. It runs on your machine, with your key,
                under your permission system.
              </p>
              <p className="label mt-5 text-band-dim">MIT licensed</p>
            </div>
            <div className="grid grid-cols-2 gap-8 sm:grid-cols-3">
              <FooterCol
                title="Documentation"
                links={[
                  ["Getting started", "/docs"],
                  ["Usage", "/docs/usage"],
                  ["Configuration", "/docs/configuration"],
                  ["Providers", "/docs/providers"],
                ]}
              />
              <FooterCol
                title="Project"
                links={[
                  ["Headless and CI", "/docs/headless"],
                  ["Security", "/docs/security"],
                  ["Source", "https://github.com/Chmgx81/opcode"],
                  ["Releases", "https://github.com/Chmgx81/opcode/releases"],
                ]}
              />
              <FooterCol
                title="Legal"
                links={[
                  ["Privacy", "/privacy"],
                  ["Terms", "/terms"],
                  ["Licence", "https://github.com/Chmgx81/opcode/blob/main/LICENSE"],
                ]}
              />
            </div>
          </div>

          <div className="mt-12 flex flex-col gap-3 border-t border-band-rule pt-6 text-[0.8125rem] text-band-mid sm:flex-row sm:items-baseline sm:justify-between">
            <p className="mono">
              No cookies. No analytics. One request to api.github.com to read the release number.
            </p>
            <p className="mono text-band-dim">Set in Source Serif 4 and IBM Plex Mono.</p>
          </div>
        </div>
      </footer>
    </div>
  );
}

function FooterCol({ title, links }: { title: string; links: [string, string][] }) {
  return (
    <div>
      <p className="label text-band-dim">{title}</p>
      <ul className="mt-3 space-y-2">
        {links.map(([label, to]) => (
          <li key={label}>
            {to.startsWith("http") ? (
              <a
                href={to}
                className="text-[0.9375rem] text-band-mid hover:text-band-fg hover:underline hover:decoration-band-accent hover:underline-offset-4"
              >
                {label}
              </a>
            ) : (
              <Link
                to={to}
                className="text-[0.9375rem] text-band-mid hover:text-band-fg hover:underline hover:decoration-band-accent hover:underline-offset-4"
              >
                {label}
              </Link>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}

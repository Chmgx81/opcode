import { useEffect, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { ArrowRight, GitHub, Menu, Moon, Sun, X } from "react-feather";

import { Release } from "@/components/ui/primitives";
import { useTheme, type Theme } from "@/lib/theme";
import { cn } from "@/lib/utils";

const nav = [
  { to: "/docs", label: "Documentation" },
  { to: "/docs/security", label: "Security" },
  { to: "/docs/headless", label: "Headless" },
];

/** The mobile menu carries GitHub too, which the desktop masthead shows as an icon. */
const mobileNav: { to?: string; href?: string; label: string }[] = [
  ...nav,
  { href: "https://github.com/Chmgx81/opcode", label: "GitHub" },
];

/**
 * The nav target that owns this path. The longest match wins, so
 * /docs/security marks Security rather than both Security and Documentation.
 */
function activeNav(pathname: string): string | undefined {
  return nav
    .filter((l) => pathname === l.to || pathname.startsWith(l.to + "/"))
    .sort((a, b) => b.to.length - a.to.length)[0]?.to;
}

/**
 * The supplied mark: a 14x8 pixel grid, drawn as rectangles so it stays crisp
 * at any size. `currentColor` rather than the asset's own media query, because
 * this one follows the site theme rather than the operating system's.
 */
export function Mark({ height = 16, className }: { height?: number; className?: string }) {
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
  const Icon = theme === "dark" ? Sun : Moon;
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-label={`Switch to ${to} mode`}
      title={`Switch to ${to} mode`}
      className="grid h-9 w-9 place-items-center rounded-ctl border border-edge text-ink-2 transition-colors hover:border-accent hover:text-accent"
    >
      <Icon size={15} aria-hidden="true" />
    </button>
  );
}

export default function Layout({ children }: { children: React.ReactNode }) {
  const [open, setOpen] = useState(false);
  const [lifted, setLifted] = useState(false);
  const { theme, toggle } = useTheme();
  const { pathname } = useLocation();
  const current = activeNav(pathname);

  useEffect(() => setOpen(false), [pathname]);

  // The masthead grows its rule once there is content behind it, so the page
  // does not open with a stray line above the fold.
  useEffect(() => {
    const onScroll = () => setLifted(window.scrollY > 8);
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  return (
    <div className="flex min-h-screen flex-col bg-paper text-ink">
      <a href="#main" className="skip">
        Skip to content
      </a>

      <header
        className={cn(
          "sticky top-0 z-40 transition-[background-color,border-color,backdrop-filter] duration-200",
          lifted
            ? "border-b border-rule bg-paper/80 backdrop-blur-xl"
            : "border-b border-transparent bg-paper",
        )}
      >
        <nav className="shell flex h-16 items-center gap-8">
          <Link
            to="/"
            className="flex shrink-0 items-center gap-2.5 text-ink transition-colors hover:text-accent"
            aria-label="opcode, home"
          >
            <Mark height={16} />
            <span className="text-[0.9375rem] font-semibold tracking-[-0.02em]">opcode</span>
          </Link>

          <div className="ml-auto flex items-center gap-2">
            <div className="hidden items-center gap-7 md:flex">
              {nav.map((l) => (
                <Link
                  key={l.to}
                  to={l.to}
                  aria-current={l.to === current ? "page" : undefined}
                  className={cn(
                    "text-[0.875rem] font-medium transition-colors",
                    l.to === current ? "text-accent" : "text-ink-2 hover:text-ink",
                  )}
                >
                  {l.label}
                </Link>
              ))}
            </div>

            <span className="mono ml-2 hidden border-l border-rule pl-6 text-[0.75rem] text-ink-3 md:inline">
              <Release />
            </span>

            <a
              href="https://github.com/Chmgx81/opcode"
              aria-label="opcode on GitHub"
              title="opcode on GitHub"
              className="hidden h-9 w-9 place-items-center rounded-ctl border border-edge text-ink-2 transition-colors hover:border-accent hover:text-accent md:grid"
            >
              <GitHub size={15} aria-hidden="true" />
            </a>

            <ThemeToggle theme={theme} onToggle={toggle} />

            <button
              type="button"
              className="grid h-9 w-9 place-items-center rounded-ctl border border-edge text-ink-2 md:hidden"
              onClick={() => setOpen(!open)}
              aria-expanded={open}
              aria-controls="mobile-nav"
              aria-label={open ? "Close menu" : "Open menu"}
            >
              {open ? <X size={16} aria-hidden="true" /> : <Menu size={16} aria-hidden="true" />}
            </button>
          </div>
        </nav>

        {open && (
          <div id="mobile-nav" className="border-t border-rule bg-paper md:hidden">
            <ul className="shell py-2">
              {mobileNav.map((l) => (
                <li key={l.label} className="border-b border-rule last:border-b-0">
                  {l.href ? (
                    <a
                      href={l.href}
                      className="flex items-center justify-between py-3.5 text-[0.9375rem] text-ink-2"
                    >
                      {l.label}
                      <ArrowRight size={15} aria-hidden="true" />
                    </a>
                  ) : (
                    <Link
                      to={l.to!}
                      aria-current={l.to === current ? "page" : undefined}
                      className={cn(
                        "block py-3.5 text-[0.9375rem]",
                        l.to === current ? "text-accent" : "text-ink-2",
                      )}
                    >
                      {l.label}
                    </Link>
                  )}
                </li>
              ))}
            </ul>
          </div>
        )}
      </header>

      <main id="main" className="flex-1">
        {children}
      </main>

      <footer className="border-t border-band-rule bg-band text-band-fg">
        <div className="shell py-16">
          <div className="grid gap-12 md:grid-cols-[minmax(0,1fr)_minmax(0,1.6fr)]">
            <div className="max-w-[30ch]">
              {/* The bare mark, not the tile: the tile is slate on a near-black
                  plate and simply disappears, so the footer would show a mark
                  with no container while the masthead showed the same mark
                  with one. Consistency beats the asset here. */}
              <Mark height={26} className="text-band-fg" />
              <p className="mt-5 text-[0.9375rem] leading-relaxed text-band-mid">
                A terminal coding agent in one Go binary. It runs on your machine, with your key,
                under your permission system.
              </p>
              <a
                href="https://github.com/Chmgx81/opcode"
                className="more mt-6 text-band-fg"
              >
                <GitHub size={15} aria-hidden="true" /> Source on GitHub
              </a>
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

          <div className="mt-14 flex flex-col gap-3 border-t border-band-rule pt-6 text-[0.8125rem] text-band-mid sm:flex-row sm:items-baseline sm:justify-between">
            <p className="mono">
              No cookies. No analytics. One request to api.github.com to read the release number.
            </p>
            <p className="mono text-band-dim">Set in Inter and IBM Plex Mono.</p>
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
      <ul className="mt-4 space-y-2.5">
        {links.map(([label, to]) => (
          <li key={label}>
            {to.startsWith("http") ? (
              <a
                href={to}
                className="text-[0.9375rem] text-band-mid transition-colors hover:text-band-fg"
              >
                {label}
              </a>
            ) : (
              <Link
                to={to}
                className="text-[0.9375rem] text-band-mid transition-colors hover:text-band-fg"
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

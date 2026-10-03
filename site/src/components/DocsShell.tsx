import { NavLink } from "react-router-dom";

const nav = [
  { to: "/docs", label: "Getting started", end: true },
  { to: "/docs/usage", label: "Usage", end: false },
  { to: "/docs/configuration", label: "Configuration", end: false },
  { to: "/docs/providers", label: "Providers", end: false },
  { to: "/docs/security", label: "Security", end: false },
  { to: "/docs/headless", label: "Headless and CI", end: false },
];

export default function DocsShell({
  children,
  source,
}: {
  children: React.ReactNode;
  source: string;
}) {
  return (
    <div className="mx-auto max-w-6xl px-6">
      <div className="grid gap-x-10 gap-y-8 py-10 lg:grid-cols-[13rem_minmax(0,1fr)] lg:py-14">
        <aside className="lg:border-r lg:border-rule lg:pr-8">
          <p className="label border-b border-rule pb-3 text-ink-3">Documentation</p>
          <nav className="mt-3 flex flex-wrap gap-x-5 gap-y-1 lg:flex-col" aria-label="Docs">
            {nav.map((n) => (
              <NavLink
                key={n.to}
                to={n.to}
                end={n.end}
                className={({ isActive }) =>
                  `mono py-1.5 text-[0.8125rem] ${
                    isActive ? "font-medium text-accent" : "text-ink-2 hover:text-accent"
                  }`
                }
              >
                {n.label}
              </NavLink>
            ))}
          </nav>
        </aside>

        <article className="prose-doc min-w-0 max-w-3xl">
          {children}
          <p className="mt-12 border-t border-rule pt-4 text-[0.8125rem] text-ink-3">
            Source:{" "}
            <a
              className="text-ink underline decoration-accent decoration-1 underline-offset-4 hover:text-accent"
              href={source}
            >
              {source.replace("https://github.com/Chmgx81/opcode/blob/main/", "")}
            </a>
            . Every claim on this page is checked against the code.
          </p>
        </article>
      </div>
    </div>
  );
}

import { NavLink } from "react-router-dom";

const nav = [
  { to: "/docs", label: "Getting started", end: true },
  { to: "/docs/usage", label: "Usage", end: false },
  { to: "/docs/configuration", label: "Configuration", end: false },
  { to: "/docs/providers", label: "Providers", end: false },
  { to: "/docs/security", label: "Security", end: false },
  { to: "/docs/headless", label: "Headless & CI", end: false },
];

export default function DocsShell({
  children,
  source,
}: {
  children: React.ReactNode;
  source: string;
}) {
  return (
    <div className="mx-auto grid max-w-6xl gap-10 px-6 pb-16 pt-12 lg:grid-cols-[13rem_1fr]">
      <aside>
        <nav className="flex flex-wrap gap-x-5 gap-y-1 text-sm lg:sticky lg:top-24 lg:flex-col" aria-label="Docs">
          {nav.map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              end={n.end}
              className={({ isActive }) =>
                `py-1 transition-colors ${isActive ? "font-semibold text-[#52a8ff]" : "text-neutral-400 hover:text-white"}`
              }
            >
              {n.label}
            </NavLink>
          ))}
        </nav>
      </aside>
      <article className="prose-doc max-w-3xl">
        {children}
        <p className="mt-10 border-t border-white/[0.08] pt-4 text-[13px] text-neutral-600">
          Source: <a className="text-[#52a8ff] hover:underline" href={source}>{source.replace("https://github.com/Chmgx81/opcode/blob/main/", "")}</a> — every claim on this page is checked against the code.
        </p>
      </article>
    </div>
  );
}

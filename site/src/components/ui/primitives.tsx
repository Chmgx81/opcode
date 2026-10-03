import { useEffect, useState } from "react";

import { cn } from "@/lib/utils";

/**
 * A numbered section. The number lives in the margin, not in a badge,
 * so the page reads as a document with a table of contents rather than
 * a stack of cards.
 */
export function Section({
  id,
  n,
  title,
  lede,
  children,
  tone = "paper",
  className,
}: {
  id?: string;
  n: string;
  title: string;
  lede?: string;
  children: React.ReactNode;
  tone?: "paper" | "recessed" | "ink";
  className?: string;
}) {
  const onInk = tone === "ink";
  return (
    <section
      id={id}
      className={cn(
        "border-t",
        onInk ? "border-band bg-band text-band-fg" : tone === "recessed" ? "border-rule bg-paper-2" : "border-rule",
        className,
      )}
    >
      <div className="mx-auto grid max-w-6xl gap-x-8 gap-y-6 px-6 py-16 md:grid-cols-[5.5rem_minmax(0,1fr)] md:py-24">
        <div className="md:pt-2">
          <span className={cn("label", onInk ? "text-band-dim" : "text-accent")}>{n}</span>
        </div>
        <div>
          <h2 className="max-w-[20ch] text-[clamp(1.75rem,3.2vw,2.6rem)] font-semibold leading-[1.08] tracking-[-0.02em]">
            {title}
          </h2>
          {lede && (
            <p className={cn("mt-4 max-w-[58ch] text-[1.0625rem] leading-relaxed", onInk ? "text-band-mid" : "text-ink-2")}>
              {lede}
            </p>
          )}
          <div className="mt-9">{children}</div>
        </div>
      </div>
    </section>
  );
}

/**
 * A figure is real output from the binary, in a hairline box, with a
 * caption. It is never dressed as a window: no title bar, no dots, no
 * radius, no shadow.
 */
export function Figure({
  fig,
  title,
  source,
  children,
}: {
  fig: string;
  title: string;
  source?: string;
  children: string;
}) {
  return (
    <figure className="mt-8">
      <pre className="capture" tabIndex={0} aria-label={`${fig} ${title}`}>
        {children}
      </pre>
      <figcaption className="caption">
        <b>{fig}.</b> {title}
        {source && (
          <>
            {" "}
            <span>Recorded from {source}.</span>
          </>
        )}
      </figcaption>
    </figure>
  );
}

/** The install line, with a copy control that reports all three states. */
export function Install({ cmd }: { cmd: string }) {
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle");

  async function copy() {
    try {
      await navigator.clipboard.writeText(cmd);
      setState("copied");
    } catch {
      setState("failed");
    }
    window.setTimeout(() => setState("idle"), 2400);
  }

  return (
    <div className="flex w-full min-w-0 items-stretch border border-ink bg-paper sm:flex-1">
      <code className="flex-1 overflow-x-auto border-0 bg-transparent px-4 py-3 text-[0.8125rem] whitespace-pre">
        {cmd}
      </code>
      <button
        type="button"
        onClick={copy}
        className="label shrink-0 border-l border-ink bg-ink px-4 text-paper hover:bg-accent"
      >
        {state === "copied" ? "copied" : state === "failed" ? "select it" : "copy"}
      </button>
      <span role="status" aria-live="polite" className="sr-only">
        {state === "copied" ? "Command copied to the clipboard" : state === "failed" ? "Copy failed" : ""}
      </span>
    </div>
  );
}

/**
 * The latest release, read live. This is the only request the page
 * makes, and it has a designed loading state, a designed value, and a
 * designed failure: a rate-limited API must not leave a broken badge.
 */
export function Release({ fallback = "v0.6.0" }: { fallback?: string }) {
  const [state, setState] = useState<"loading" | "live" | "offline">("loading");
  const [version, setVersion] = useState(fallback);

  useEffect(() => {
    let alive = true;
    const controller = new AbortController();
    fetch("https://api.github.com/repos/Chmgx81/opcode/releases/latest", {
      signal: controller.signal,
      headers: { Accept: "application/vnd.github+json" },
    })
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error(String(r.status)))))
      .then((d: { tag_name?: string }) => {
        if (!alive) return;
        if (typeof d.tag_name === "string") {
          setVersion(d.tag_name);
          setState("live");
        } else {
          setState("offline");
        }
      })
      .catch(() => {
        /* rate limited or offline: the last known release stands */
        if (alive) setState("offline");
      });
    return () => {
      alive = false;
      controller.abort();
    };
  }, []);

  if (state === "loading") return <span className="skeleton" aria-label="reading the latest release" />;
  return (
    <span title={state === "offline" ? "GitHub could not be reached; showing the last known release" : undefined}>
      {version}
    </span>
  );
}

/** A ruled data row. Used for facts, modes, providers, shortcuts. */
export function Ruled({
  head,
  rows,
  className,
}: {
  head?: string[];
  rows: React.ReactNode[][];
  className?: string;
}) {
  return (
    <div className={cn("overflow-x-auto", className)}>
      <table className="w-full border-collapse text-left">
        {head && (
          <thead>
            <tr>
              {head.map((h) => (
                <th key={h} className="label border-b border-rule-2 pb-2 pr-6 text-ink-3">
                  {h}
                </th>
              ))}
            </tr>
          </thead>
        )}
        <tbody>
          {rows.map((row, i) => (
            <tr key={i} className="border-b border-rule last:border-b-0">
              {row.map((cell, j) => (
                <td
                  key={j}
                  className={cn(
                    "py-3 pr-6 align-top text-[0.9375rem] leading-relaxed",
                    j === 0 ? "mono text-[0.8125rem] text-ink" : "text-ink-2",
                  )}
                >
                  {cell}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

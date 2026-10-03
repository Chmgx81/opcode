import { useEffect, useRef, useState } from "react";
import { Check, ChevronDown, Copy } from "react-feather";

import { cn } from "@/lib/utils";

/**
 * Scroll-triggered entrance for everything below the fold. One observer per
 * element is fine at this page size and keeps the primitive dependency-free.
 *
 * `data-shown` is set on first intersection, and the stylesheet pins `.reveal`
 * visible under `prefers-reduced-motion`, so content can never be stranded
 * invisible by a missing observer or a reader who asked for stillness.
 */
export function Reveal({
  children,
  className,
  delay = 0,
  as: Tag = "div",
}: {
  children: React.ReactNode;
  className?: string;
  delay?: number;
  as?: "div" | "section" | "li" | "article" | "header";
}) {
  const ref = useRef<HTMLElement>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (typeof IntersectionObserver === "undefined") {
      el.dataset.shown = "true";
      return;
    }
    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (e.isIntersecting) {
            el.dataset.shown = "true";
            io.disconnect();
          }
        }
      },
      { rootMargin: "0px 0px -10% 0px", threshold: 0.04 },
    );
    io.observe(el);
    return () => io.disconnect();
  }, []);

  return (
    <Tag ref={ref as never} className={cn("reveal", className)} style={{ "--d": delay } as never}>
      {children}
    </Tag>
  );
}

/**
 * A page band. `title` and `lede` form the standard heading block; `aside`
 * sets them side by side, which is the pattern Grok Build and Neo use for
 * their longer sections and which keeps the measure readable.
 */
export function Section({
  id,
  eyebrow,
  title,
  lede,
  aside,
  children,
  tone = "paper",
  className,
}: {
  id?: string;
  eyebrow?: string;
  title?: string;
  lede?: string;
  aside?: React.ReactNode;
  children?: React.ReactNode;
  tone?: "paper" | "raised" | "band";
  className?: string;
}) {
  const onBand = tone === "band";
  return (
    <section
      id={id}
      className={cn(
        onBand
          ? "border-t border-band-rule bg-band text-band-fg"
          : tone === "raised"
            ? "border-t border-rule bg-paper-2"
            : "border-t border-rule",
        className,
      )}
    >
      <div className="shell py-20 md:py-28">
        {(title || lede) && (
          <Reveal
            className={cn(
              "flex flex-col gap-6 md:flex-row md:items-end md:justify-between md:gap-16",
              aside ? "" : "max-w-3xl",
            )}
          >
            <div className={cn("min-w-0", aside ? "md:max-w-[20ch] lg:max-w-[24ch]" : "flex-1")}>
              {eyebrow && (
                <p className={cn("label mb-4", onBand ? "text-accent" : "text-accent")}>{eyebrow}</p>
              )}
              {title && (
                <h2
                  className={cn(
                    "h-section text-[clamp(1.875rem,4vw,3rem)]",
                    onBand ? "text-band-fg" : "text-ink",
                  )}
                >
                  {title}
                </h2>
              )}
            </div>
            {(lede || aside) && (
              <div className={cn("min-w-0", aside ? "md:flex-1" : "flex-1")}>
                {lede && (
                  <p className={cn("lede max-w-[52ch]", onBand ? "text-band-mid" : "text-ink-2")}>
                    {lede}
                  </p>
                )}
                {aside}
              </div>
            )}
          </Reveal>
        )}
        {children && <div className={cn(title || lede ? "mt-14 md:mt-16" : undefined)}>{children}</div>}
      </div>
    </section>
  );
}

/**
 * A framed terminal. The window chrome is a device convention that every
 * reference uses to present product output; the bytes inside are verbatim
 * release-build output, and the binary's own ASCII banner sits directly under
 * the bar so nothing is cropped or restyled into meaning it did not have.
 *
 * `live` adds a blinking caret on the hero frame only.
 */
export function Terminal({
  title,
  children,
  live = false,
  hero = false,
  className,
}: {
  title?: string;
  children: string;
  live?: boolean;
  hero?: boolean;
  className?: string;
}) {
  return (
    <div className={cn("term", hero && "term--hero", live && "term--live", className)}>
      <div className="term__bar">
        <span className="term__dot" />
        <span className="term__dot" />
        <span className="term__dot" />
        {title && <span className="term__title">{title}</span>}
      </div>
      <pre className="term__body" tabIndex={0}>
        {children}
      </pre>
    </div>
  );
}

/** A terminal plus its caption. The pair is always used together. */
export function Figure({
  fig,
  title,
  source,
  children,
  hero = false,
  live = false,
}: {
  fig: string;
  title: string;
  source?: string;
  children: string;
  hero?: boolean;
  live?: boolean;
}) {
  return (
    <figure className="m-0 min-w-0">
      <Terminal title={fig} hero={hero} live={live}>
        {children}
      </Terminal>
      <figcaption className="caption">
        <b>{fig}.</b> {title}
        {source && <> Recorded from {source}.</>}
      </figcaption>
    </figure>
  );
}

/**
 * The install command, with a copy control that reports all three states. A
 * clipboard rejection is a real outcome on a locked-down browser, and it has to
 * say so rather than silently pretending it worked.
 */
export function Install({ cmd }: { cmd: string }) {
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle");
  const timer = useRef<number | undefined>(undefined);

  useEffect(() => () => window.clearTimeout(timer.current), []);

  async function copy() {
    window.clearTimeout(timer.current);
    try {
      await navigator.clipboard.writeText(cmd);
      setState("copied");
    } catch {
      setState("failed");
    }
    timer.current = window.setTimeout(() => setState("idle"), 2400);
  }

  return (
    <div className="cmd">
      <pre className="cmd__text">{cmd}</pre>
      <button
        type="button"
        onClick={copy}
        data-state={state}
        className="cmd__btn"
        aria-label={state === "failed" ? "Copy blocked, select the command instead" : "Copy install command"}
      >
        {state === "copied" ? (
          <>
            <Check size={13} aria-hidden="true" /> Copied
          </>
        ) : state === "failed" ? (
          "Select it"
        ) : (
          <>
            <Copy size={13} aria-hidden="true" /> Copy
          </>
        )}
      </button>
      <span role="status" aria-live="polite" className="sr-only">
        {state === "copied"
          ? "Install command copied to the clipboard"
          : state === "failed"
            ? "The browser blocked clipboard access. Select the command and copy it manually."
            : ""}
      </span>
    </div>
  );
}

/**
 * The latest release, read live. This is the only request the page makes, and
 * it has a designed loading state, a designed value and a designed failure: a
 * rate-limited API must not leave a broken badge.
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
    <span
      title={
        state === "offline" ? "GitHub could not be reached; showing the last known release" : undefined
      }
    >
      {version}
    </span>
  );
}

/**
 * The icon, title and body triple used across every feature card and row, with
 * the internal name last as a mono footer. The order is Cline's: marker,
 * claim, explanation, then the precise term. Putting the term between the title
 * and the body reads as a stray label; putting it last reads as a caption.
 */
export function Feature({
  icon,
  title,
  term,
  children,
  className,
}: {
  icon: React.ReactNode;
  title: string;
  term?: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex h-full flex-col", className)}>
      {/* The icon repeats the title, so it is decoration: the title is what a
          screen reader should announce, not an unlabelled graphic before it. */}
      <span className="icon-well" aria-hidden="true">
        {icon}
      </span>
      <h3 className="h-card mt-5 text-[1.0625rem] text-ink">{title}</h3>
      <div className="mt-2.5 text-[0.9375rem] leading-relaxed text-ink-2">{children}</div>
      {term && <p className="label mt-5 text-ink-3">{term}</p>}
    </div>
  );
}

/** A ruled data row. Used for modes, providers and shortcuts. */
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
                <th key={h} className="label border-b border-rule-2 pb-3 pr-8 text-ink-3">
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
                    "py-4 pr-8 align-top text-[0.9375rem] leading-relaxed",
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

/** A question and its answer. Height animates from 0fr to 1fr in CSS. */
export function QA({ q, a }: { q: string; a: string }) {
  return (
    <details className="qa group">
      <summary>
        <span>{q}</span>
        <ChevronDown size={18} className="qa__chev" aria-hidden="true" />
      </summary>
      <div className="qa__wrap">
        <div className="qa__inner">
          <p className="body-copy">{a}</p>
        </div>
      </div>
    </details>
  );
}

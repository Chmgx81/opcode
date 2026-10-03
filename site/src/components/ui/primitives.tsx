import { useEffect, useState } from "react";

import { cn } from "@/lib/utils";

/**
 * A numbered section with enhanced styling.
 * The number lives in the margin with a gradient accent,
 * creating a modern, structured document feel.
 */
export function Section({
  id,
  n,
  title,
  lede,
  children,
  tone = "paper",
  className,
  fullWidth = false,
}: {
  id?: string;
  n: string;
  title: string;
  lede?: string;
  children: React.ReactNode;
  tone?: "paper" | "recessed" | "ink";
  className?: string;
  fullWidth?: boolean;
}) {
  const onInk = tone === "ink";
  const onRecessed = tone === "recessed";
  
  return (
    <section
      id={id}
      className={cn(
        "border-t",
        onInk ? "border-band bg-band text-band-fg" : 
        onRecessed ? "border-rule bg-paper-2" : 
        "border-rule",
        className,
      )}
    >
      <div className={cn(
        "mx-auto grid max-w-6xl gap-x-8 gap-y-6 px-6 py-16 md:grid-cols-[5.5rem_minmax(0,1fr)] md:py-24",
        fullWidth && "max-w-none px-4 sm:px-6 lg:px-8"
      )}>
        <div className="md:pt-2">
          <span className={cn(
            "label inline-block px-2 py-1",
            onInk ? "text-band-dim bg-band-rule" : 
            onRecessed ? "text-ink-3 bg-paper-3" :
            "text-accent bg-accent-3 bg-opacity-20"
          )}>
            {n}
          </span>
        </div>
        <div>
          <h2 className={cn(
            "max-w-[24ch] text-[clamp(1.75rem,3.2vw,2.6rem)] font-semibold leading-[1.08] tracking-[-0.02em]",
            onInk ? "text-band-fg" : "text-ink"
          )}>
            {title}
          </h2>
          {lede && (
            <p className={cn(
              "mt-4 max-w-[58ch] text-[1.0625rem] leading-relaxed",
              onInk ? "text-band-mid" : "text-ink-2"
            )}>
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
 * A figure component for terminal output with enhanced styling.
 * Real output from the binary, in a hairline box, with a caption.
 * Now includes a subtle glow effect on hover.
 */
export function Figure({
  fig,
  title,
  source,
  children,
  className,
  interactive = false,
}: {
  fig: string;
  title: string;
  source?: string;
  children: string;
  className?: string;
  interactive?: boolean;
}) {
  return (
    <figure className={cn("mt-8", className)}>
      <pre 
        className={cn(
          "capture transition-all duration-200",
          interactive && "cursor-pointer hover:scale-[1.01]"
        )}
        tabIndex={0} 
        aria-label={`${fig} ${title}`}
      >
        {children}
      </pre>
      <figcaption className="caption">
        <b className="text-accent">{fig}.</b> {title}
        {source && (
          <>
            {" "}
            <span className="text-ink-4">Recorded from {source}.</span>
          </>
        )}
      </figcaption>
    </figure>
  );
}

/**
 * Enhanced install line component with copy control.
 * Now includes success/failure states with icons and better feedback.
 */
export function Install({ cmd, className }: { cmd: string; className?: string }) {
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle");

  async function copy() {
    try {
      await navigator.clipboard.writeText(cmd);
      setState("copied");
      setTimeout(() => setState("idle"), 3000);
    } catch {
      setState("failed");
      setTimeout(() => setState("idle"), 3000);
    }
  }

  const getIcon = () => {
    if (state === "copied") return "✓";
    if (state === "failed") return "✗";
    return null;
  };

  const getLabel = () => {
    if (state === "copied") return "Copied!";
    if (state === "failed") return "Copy failed";
    return "Copy";
  };

  return (
    <div className={cn(
      "flex w-full min-w-0 items-stretch border border-ink bg-paper transition-all duration-200",
      state === "copied" && "border-accent bg-accent bg-opacity-5",
      className
    )}>
      <code className="flex-1 overflow-x-auto border-0 bg-transparent px-4 py-3 text-[0.8125rem] whitespace-pre">
        {cmd}
      </code>
      <button
        type="button"
        onClick={copy}
        className={cn(
          "label shrink-0 border-l border-ink bg-ink px-4 py-3 text-paper transition-all duration-200 hover:bg-accent",
          state === "copied" && "bg-accent border-accent",
          state === "failed" && "bg-error border-error"
        )}
      >
        {getIcon() && <span aria-hidden="true">{getIcon()}</span>}
        <span className={getIcon() ? "ml-1" : ""}>{getLabel()}</span>
      </button>
      <span role="status" aria-live="polite" className="sr-only">
        {state === "copied" ? "Command copied to the clipboard" : 
         state === "failed" ? "Copy failed, please try again" : ""}
      </span>
    </div>
  );
}

/**
 * Enhanced release component with better loading state.
 * Now includes version comparison and better error handling.
 */
export function Release({ fallback = "v0.6.0", showVersion = true }: { fallback?: string; showVersion?: boolean }) {
  const [state, setState] = useState<"loading" | "live" | "offline">("loading");
  const [version, setVersion] = useState(fallback);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    const controller = new AbortController();
    
    fetch("https://api.github.com/repos/Chmgx81/opcode/releases/latest", {
      signal: controller.signal,
      headers: { Accept: "application/vnd.github+json" },
    })
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json();
      })
      .then((d: { tag_name?: string; name?: string }) => {
        if (!alive) return;
        const releaseVersion = d.tag_name || d.name || fallback;
        if (typeof releaseVersion === "string") {
          setVersion(releaseVersion);
          setState("live");
        } else {
          setState("offline");
        }
      })
      .catch((err) => {
        if (!alive) return;
        setError(err.message);
        setState("offline");
      });
    
    return () => {
      alive = false;
      controller.abort();
    };
  }, []);

  if (state === "loading") {
    return (
      <span 
        className="skeleton" 
        aria-label="Reading the latest release"
        style={{ width: "5ch", height: "0.9em" }}
      />
    );
  }

  if (showVersion) {
    return (
      <span 
        className="inline-flex items-center gap-2"
        title={state === "offline" ? error || "GitHub could not be reached; showing the last known release" : `Latest release: ${version}`}
      >
        <span className={cn(
          "text-[0.8125rem] font-medium",
          state === "live" ? "text-accent" : "text-ink-3"
        )}>
          {version}
        </span>
        {state === "live" && (
          <span className="text-[0.6875rem] text-accent-2" aria-hidden="true">
            Latest
          </span>
        )}
      </span>
    );
  }

  return <>{version}</>;
}

/**
 * Enhanced ruled data row with hover effects and better spacing.
 * Used for facts, modes, providers, shortcuts.
 */
export function Ruled({
  head,
  rows,
  className,
  hoverable = false,
}: {
  head?: string[];
  rows: React.ReactNode[][];
  className?: string;
  hoverable?: boolean;
}) {
  return (
    <div className={cn("overflow-x-auto rounded-sm", className)}>
      <table className="w-full border-collapse text-left">
        {head && (
          <thead>
            <tr>
              {head.map((h) => (
                <th 
                  key={h} 
                  className="label border-b-2 border-rule-2 pb-3 pr-6 text-ink-3 font-medium"
                >
                  {h}
                </th>
              ))}
            </tr>
          </thead>
        )}
        <tbody>
          {rows.map((row, i) => (
            <tr 
              key={i} 
              className={cn(
                "border-b border-rule last:border-b-0 transition-colors duration-150",
                hoverable && "hover:bg-paper-2"
              )}
            >
              {row.map((cell, j) => (
                <td
                  key={j}
                  className={cn(
                    "py-3 pr-6 align-top text-[0.9375rem] leading-relaxed transition-colors duration-150",
                    j === 0 ? "mono text-[0.8125rem] text-ink font-medium" : "text-ink-2",
                    hoverable && "group"
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

/**
 * New: Feature highlight card component
 * Used to showcase individual features with icons and descriptions
 */
export function FeatureCard({
  icon,
  title,
  description,
  className,
  delay = 0,
}: {
  icon: React.ReactNode;
  title: string;
  description: string;
  className?: string;
  delay?: number;
}) {
  return (
    <div 
      className={cn(
        "card group p-6 transition-all duration-300",
        className
      )}
      style={{ animationDelay: `${delay}ms` }}
    >
      <div className="mb-4 text-accent text-2xl">{icon}</div>
      <h3 className="text-lg font-semibold text-ink mb-2 group-hover:text-accent transition-colors duration-200">
        {title}
      </h3>
      <p className="text-ink-2 leading-relaxed">{description}</p>
    </div>
  );
}

/**
 * New: Comparison table component for competitor analysis
 */
export function ComparisonTable({
  features,
  competitors,
  className,
}: {
  features: string[];
  competitors: { name: string; values: (boolean | string)[] }[];
  className?: string;
}) {
  return (
    <div className={cn("overflow-x-auto", className)}>
      <table className="w-full border-collapse">
        <thead>
          <tr>
            <th className="label border-b-2 border-rule-2 pb-3 pr-6 text-left text-ink-3 font-medium">
              Feature
            </th>
            {competitors.map((c) => (
              <th 
                key={c.name}
                className="label border-b-2 border-rule-2 pb-3 pr-6 text-center text-ink-3 font-medium"
              >
                {c.name}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {features.map((feature, i) => (
            <tr key={i} className="border-b border-rule last:border-b-0">
              <td className="py-3 pr-6 text-ink font-medium text-[0.9375rem]">
                {feature}
              </td>
              {competitors.map((c, j) => (
                <td 
                  key={j}
                  className="py-3 pr-6 text-center text-[0.9375rem]"
                >
                  {typeof c.values[i] === 'boolean' ? (
                    c.values[i] as boolean ? (
                      <span className="text-success font-medium">✓</span>
                    ) : (
                      <span className="text-ink-4">✗</span>
                    )
                  ) : (
                    <span className="text-ink-2">{c.values[i] as string}</span>
                  )}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/**
 * New: Hero terminal component that simulates a live terminal
 */
export function HeroTerminal({
  lines,
  className,
  typingSpeed = 50,
}: {
  lines: string[];
  className?: string;
  typingSpeed?: number;
}) {
  const [displayedLines, setDisplayedLines] = useState<string[]>([]);
  const [currentLineIndex, setCurrentLineIndex] = useState(0);
  const [isTyping, setIsTyping] = useState(false);

  useEffect(() => {
    let timeoutId: ReturnType<typeof setTimeout>;
    let currentIndex = 0;
    let currentLine = "";

    const typeLine = () => {
      if (currentIndex < lines.length) {
        const line = lines[currentIndex];
        setIsTyping(true);
        
        for (let i = 0; i <= line.length; i++) {
          timeoutId = setTimeout(() => {
            currentLine = line.substring(0, i);
            setDisplayedLines(prev => {
              const newLines = [...prev];
              newLines[currentIndex] = currentLine;
              return newLines;
            });
          }, i * typingSpeed);
        }

        currentIndex++;
        timeoutId = setTimeout(() => {
          setDisplayedLines(prev => [...prev, ""]);
          setCurrentLineIndex(currentIndex);
          setIsTyping(false);
          if (currentIndex < lines.length) {
            typeLine();
          }
        }, line.length * typingSpeed + 500);
      }
    };

    typeLine();

    return () => clearTimeout(timeoutId);
  }, [lines, typingSpeed]);

  return (
    <pre className={cn(
      "capture font-mono text-[0.875rem] leading-[1.7]",
      className
    )}>
      {displayedLines.map((line, index) => (
        <div key={index} className={cn(
          "transition-opacity duration-200",
          index === currentLineIndex && isTyping ? "opacity-100" : "opacity-100"
        )}>
          <span className="text-accent">$</span> {line}
        </div>
      ))}
      {isTyping && currentLineIndex < lines.length && (
        <div className="blink">|</div>
      )}
    </pre>
  );
}

/**
 * New: Badge component for highlighting features
 */
export function Badge({
  children,
  variant = "primary",
  className,
}: {
  children: React.ReactNode;
  variant?: "primary" | "secondary" | "accent";
  className?: string;
}) {
  return (
    <span className={cn(
      "label inline-flex items-center gap-1 px-2 py-1",
      variant === "primary" && "bg-ink text-paper",
      variant === "secondary" && "bg-paper-2 text-ink border border-rule",
      variant === "accent" && "bg-accent text-paper",
      className
    )}>
      {children}
    </span>
  );
}

/**
 * New: Stat card for displaying metrics
 */
export function StatCard({
  value,
  label,
  icon,
  className,
}: {
  value: string;
  label: string;
  icon?: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn(
      "card text-center p-6 transition-all duration-300 hover:shadow-md",
      className
    )}>
      {icon && <div className="text-accent text-3xl mb-4">{icon}</div>}
      <dt className="mono text-[clamp(1.75rem,3.5vw,2.5rem)] leading-none tracking-[-0.04em] text-ink">
        {value}
      </dt>
      <dd className="mt-3 max-w-[22ch] mx-auto text-[0.8125rem] leading-snug text-ink-3">
        {label}
      </dd>
    </div>
  );
}

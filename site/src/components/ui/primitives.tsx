import * as React from "react";
import { motion, useReducedMotion } from "motion/react";
import Balancer from "react-wrap-balancer";

import { cn } from "@/lib/utils";

// Reveal: motion-driven, reduced-motion aware, staggered on scroll.
export function Reveal({
  children,
  className,
  delay = 0,
}: {
  children: React.ReactNode;
  className?: string;
  delay?: number;
}) {
  const reduce = useReducedMotion();
  if (reduce) return <div className={className}>{children}</div>;
  return (
    <motion.div
      className={className}
      initial={{ opacity: 0, y: 14, filter: "blur(5px)" }}
      whileInView={{ opacity: 1, y: 0, filter: "blur(0px)" }}
      viewport={{ once: true, margin: "-60px" }}
      transition={{ duration: 0.55, ease: [0.22, 1, 0.36, 1], delay }}
    >
      {children}
    </motion.div>
  );
}

// Section: the eyebrow/kicker + big tracking-tighter title + lede.
export function SectionHeader({
  kicker,
  title,
  lede,
  className,
}: {
  kicker: string;
  title: string;
  lede?: string;
  className?: string;
}) {
  return (
    <Reveal className={cn("max-w-2xl", className)}>
      <p className="font-mono text-xs uppercase tracking-[0.14em] text-[#52a8ff]">
        {kicker}
      </p>
      <h2 className="mt-3 text-balance text-3xl font-medium tracking-tighter text-white sm:text-5xl">
        <Balancer>{title}</Balancer>
      </h2>
      {lede && <p className="mt-4 text-base text-neutral-400">{lede}</p>}
    </Reveal>
  );
}

// Chip: the pill status indicator.
export function Chip({
  children,
  tone = "neutral",
  className,
}: {
  children: React.ReactNode;
  tone?: "neutral" | "ok" | "blue" | "lite";
  className?: string;
}) {
  const dot =
    tone === "ok" ? "#62c073" : tone === "blue" ? "#52a8ff" : tone === "lite" ? "#ededed" : "#525252";
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border border-white/[0.08] bg-white/[0.03] px-2.5 py-1 font-mono text-[11px] uppercase tracking-wider text-neutral-400",
        className,
      )}
    >
      <span className="h-1.5 w-1.5 rounded-full" style={{ background: dot }} />
      {children}
    </span>
  );
}

// Terminal: the mock window used across the landing sections.
export function Terminal({
  title,
  children,
  className,
}: {
  title: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "overflow-hidden rounded-xl border border-white/[0.12] bg-[#050505] shadow-2xl",
        className,
      )}
    >
      <div className="flex items-center gap-4 border-b border-white/[0.08] bg-white/[0.02] px-4 py-3">
        <div className="flex gap-1.5">
          <div className="h-2.5 w-2.5 rounded-full bg-white/[0.15]" />
          <div className="h-2.5 w-2.5 rounded-full bg-white/[0.15]" />
          <div className="h-2.5 w-2.5 rounded-full bg-white/[0.15]" />
        </div>
        <span className="mx-auto font-mono text-[11px] uppercase tracking-wider text-neutral-600">
          {title}
        </span>
      </div>
      <div className="p-5 font-mono text-[13px] leading-[1.8] sm:p-6">{children}</div>
    </div>
  );
}

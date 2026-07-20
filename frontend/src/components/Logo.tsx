// Product logo mark: indigo rounded square with a white link glyph, matching
// the marketing site header. Single source of truth — change the mark here and
// every surface (sidebar, login, onboarding, 404, …) follows.

const LINK_GLYPH =
  "M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71";

type LogoSize = "sm" | "md" | "lg";

const SIZES: Record<LogoSize, { box: string; glyph: string; word: string }> = {
  sm: { box: "h-7 w-7 rounded-lg", glyph: "h-4 w-4", word: "text-lg" },
  md: { box: "h-9 w-9 rounded-xl", glyph: "h-5 w-5", word: "text-xl" },
  lg: { box: "h-12 w-12 rounded-2xl", glyph: "h-7 w-7", word: "text-2xl" },
};

interface LogoProps {
  size?: LogoSize;
  /** Render the "ogtr" wordmark beside the mark (inherits currentColor). */
  withWordmark?: boolean;
  className?: string;
}

export default function Logo({ size = "sm", withWordmark = false, className = "" }: LogoProps) {
  const s = SIZES[size];

  const mark = (
    <span
      className={`flex items-center justify-center bg-indigo-600 text-white ${s.box} ${
        withWordmark ? "" : className
      }`}
    >
      <svg
        className={s.glyph}
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d={LINK_GLYPH} />
      </svg>
    </span>
  );

  if (!withWordmark) return mark;

  return (
    <span className={`inline-flex items-center gap-2.5 ${className}`}>
      {mark}
      <span className={`font-semibold tracking-tight ${s.word}`}>ogtr</span>
    </span>
  );
}

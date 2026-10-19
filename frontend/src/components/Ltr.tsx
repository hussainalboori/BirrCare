import React from "react";

/**
 * Isolates a numeric/clinical run (e.g. "120 / 80 mmHg", "72 bpm", "3 / 5")
 * so the Unicode bidi algorithm cannot reorder it inside Arabic text, and keeps
 * it on one line. Without isolation, "120 / 80" in an RTL paragraph renders as
 * "80 / 120" because the spaced slash is a neutral resolved to RTL.
 */
export function Ltr({
  children,
  className = "",
  ...rest
}: { children: React.ReactNode; className?: string } & React.HTMLAttributes<HTMLElement> & { "data-testid"?: string }) {
  return (
    <bdi dir="ltr" className={`whitespace-nowrap tabular-nums ${className}`.trim()} {...rest}>
      {children}
    </bdi>
  );
}

/**
 * Isolates free-form text that may be Latin or Arabic (e.g. a dosage "5 mg"
 * vs "5 ملغ"): direction is taken from the first strong character.
 */
export function Auto({ children, className = "" }: { children: React.ReactNode; className?: string }) {
  return (
    <bdi dir="auto" className={`whitespace-nowrap ${className}`.trim()}>
      {children}
    </bdi>
  );
}

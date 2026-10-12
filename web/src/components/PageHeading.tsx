import { useEffect, useRef, type ReactNode } from "react";
import { cn } from "@/lib/utils";

type Role = "display" | "heading";

// Page title. Takes focus on mount so route changes are announced and keyboard
// users start at the top of the new page (tabindex -1, no visible outline needed).
export function PageHeading({
  children,
  role = "heading",
  className,
}: {
  children: ReactNode;
  role?: Role;
  className?: string;
}) {
  const ref = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    ref.current?.focus({ preventScroll: true });
  }, []);
  return (
    <h1
      ref={ref}
      tabIndex={-1}
      className={cn(
        "leading-[1.2] font-semibold outline-none",
        role === "display" ? "text-[28px]" : "text-xl",
        className,
      )}
    >
      {children}
    </h1>
  );
}

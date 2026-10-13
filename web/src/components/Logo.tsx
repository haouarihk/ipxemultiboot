import { cn } from "./ui";

export function LogoMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" className={cn("size-8", className)} aria-hidden>
      <rect width="32" height="32" rx="8" className="fill-accent" />
      <circle cx="16" cy="16" r="9" fill="none" stroke="white" strokeWidth="2.5" />
      <circle cx="16" cy="16" r="2.5" fill="white" />
      <path d="M16 4v5M16 23v5M4 16h5M23 16h5" stroke="white" strokeOpacity="0.6" strokeWidth="2" strokeLinecap="round" />
    </svg>
  );
}

export function Logo({ className }: { className?: string }) {
  return (
    <div className={cn("flex items-center gap-2.5", className)}>
      <LogoMark />
      <span className="text-lg font-semibold tracking-tight">
        iPXE<span className="text-accent">Multiboot</span>
      </span>
    </div>
  );
}

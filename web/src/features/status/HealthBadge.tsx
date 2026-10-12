import { AlertTriangle, CheckCircle2, HelpCircle, XCircle, type LucideIcon } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

export type Health = "healthy" | "degraded" | "down" | "unknown";

// Color is never the only signal: every state has an icon and a text label (UI-SPEC).
// Colors come from theme variables only; the accent color is never used on badges.
const STATES: Record<Health, { label: string; Icon: LucideIcon; tone: string }> = {
  healthy: { label: "Healthy", Icon: CheckCircle2, tone: "text-foreground" },
  degraded: { label: "Degraded", Icon: AlertTriangle, tone: "text-warning" },
  down: { label: "Down", Icon: XCircle, tone: "text-destructive" },
  unknown: { label: "Unknown", Icon: HelpCircle, tone: "text-muted-foreground" },
};

export function HealthBadge({ status }: { status: Health }) {
  const { label, Icon, tone } = STATES[status];
  return (
    <Badge variant="secondary" className={cn("text-xs font-semibold", tone)} data-health={status}>
      <Icon aria-hidden="true" />
      {label}
    </Badge>
  );
}

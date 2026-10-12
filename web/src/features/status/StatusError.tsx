import { useEffect, useRef, useState } from "react";
import { Copy } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";

const COPIED_MS = 2000;

// Shown when /api/status fails with no cached data. The trace id lets an SRE
// paste the failed request straight into Grafana.
export function StatusError({ traceId, onRetry }: { traceId: string | null; onRetry: () => void }) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);

  const copy = async () => {
    if (!traceId) return;
    try {
      await navigator.clipboard.writeText(traceId);
    } catch {
      return; // Clipboard unavailable: the id is still visible and selectable.
    }
    setCopied(true);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setCopied(false), COPIED_MS);
  };

  return (
    <div role="alert" className="flex flex-col gap-3 text-sm">
      <p className="text-xl leading-[1.2] font-semibold">Couldn&apos;t load telemetry status</p>
      <p className="flex flex-wrap items-center gap-x-1">
        <span>
          The request to the API failed. Select Retry Loading Status
          {traceId ? ", or look up trace " : "."}
        </span>
        {traceId && (
          <>
            <code className="font-mono">{traceId}</code>
            <span>in Grafana.</span>
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label="Copy trace ID"
                    onClick={() => void copy()}
                  >
                    <Copy aria-hidden="true" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{copied ? "Trace ID copied" : "Copy trace ID"}</TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </>
        )}
      </p>
      <span aria-live="polite" className="sr-only">
        {copied ? "Trace ID copied" : ""}
      </span>
      <div>
        <Button variant="outline" onClick={onRetry}>
          Retry Loading Status
        </Button>
      </div>
    </div>
  );
}

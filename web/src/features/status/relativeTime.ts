// Compact relative time for the status page: "12s ago", "3m ago", "2h ago", "4d ago".
// Absolute timestamps belong in a title attribute; this is only the glanceable form.
export function relativeTime(ts: string | Date | null | undefined, now: number = Date.now()): string {
  if (!ts) return "—";
  const then = ts instanceof Date ? ts.getTime() : Date.parse(ts);
  if (Number.isNaN(then)) return "—";
  const secs = Math.max(0, Math.round((now - then) / 1000));
  if (secs < 60) return `${secs}s ago`;
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

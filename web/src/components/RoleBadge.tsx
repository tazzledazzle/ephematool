import { Badge } from "@/components/ui/badge";

export function RoleBadge({ role }: { role: "admin" | "viewer" }) {
  return (
    <Badge variant="secondary" className="text-xs font-semibold">
      {role === "admin" ? "Admin" : "Viewer"}
    </Badge>
  );
}

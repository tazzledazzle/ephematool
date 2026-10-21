import { Badge } from "@/components/ui/badge";
import type { components } from "@/api/schema";

// The role type comes from the generated API schema, so adding a role on the
// server is a compile error here until the label is added.
export type Role = components["schemas"]["MeBody"]["role"];

const LABEL: Record<Role, string> = {
  admin: "Admin",
  creator: "Creator",
  viewer: "Viewer",
};

export function RoleBadge({ role }: { role: Role }) {
  return (
    <Badge variant="secondary" className="text-xs font-semibold">
      {LABEL[role]}
    </Badge>
  );
}

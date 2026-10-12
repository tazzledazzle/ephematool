import { RoleBadge } from "@/components/RoleBadge";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useMe } from "@/features/auth/useMe";

export function SignedInCard() {
  const me = useMe();
  return (
    <Card role="region" aria-label="Signed in as">
      <CardHeader>
        <CardTitle className="text-xl leading-[1.2] font-semibold">Signed in as</CardTitle>
      </CardHeader>
      <CardContent>
        {me.data ? (
          <div className="flex flex-wrap items-center gap-3">
            <Avatar className="size-10">
              <AvatarImage src={me.data.avatarUrl} alt="" />
              <AvatarFallback>{me.data.login.slice(0, 1).toUpperCase()}</AvatarFallback>
            </Avatar>
            <span className="text-sm font-semibold">{me.data.login}</span>
            <RoleBadge role={me.data.role} />
            {(me.data.orgs ?? []).length > 0 && (
              <span className="text-sm text-muted-foreground">
                Access granted by {(me.data.orgs ?? []).join(", ")}
              </span>
            )}
          </div>
        ) : (
          <Skeleton className="h-10 w-full max-w-sm" />
        )}
      </CardContent>
    </Card>
  );
}

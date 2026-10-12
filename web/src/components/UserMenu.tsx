import { useState } from "react";
import { RoleBadge } from "@/components/RoleBadge";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { useTheme } from "@/lib/ThemeProvider";
import type { ThemeChoice } from "@/lib/theme";

type Props = {
  login: string;
  avatarUrl: string;
  role: "admin" | "viewer";
  onSignOut: () => Promise<void>;
};

const THEME_OPTIONS: ReadonlyArray<{ value: ThemeChoice; label: string }> = [
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
  { value: "system", label: "System" },
];

// Sign-out (POST /api/auth/logout, clear the query cache, navigate) lives in the caller so this
// component stays free of router and query-client context.
export function UserMenu({ login, avatarUrl, role, onSignOut }: Props) {
  const { choice, setChoice } = useTheme();
  const [signingOut, setSigningOut] = useState(false);

  const signOut = async () => {
    setSigningOut(true);
    try {
      await onSignOut();
    } finally {
      setSigningOut(false);
    }
  };

  return (
    <TooltipProvider>
      <DropdownMenu>
        <Tooltip>
          <TooltipTrigger asChild>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                className="size-8 rounded-full p-0"
                aria-label={`Open user menu for ${login}`}
              >
                <Avatar className="size-8">
                  <AvatarImage src={avatarUrl} alt="" />
                  <AvatarFallback>{login.slice(0, 1).toUpperCase()}</AvatarFallback>
                </Avatar>
              </Button>
            </DropdownMenuTrigger>
          </TooltipTrigger>
          <TooltipContent>Account menu</TooltipContent>
        </Tooltip>
        <DropdownMenuContent align="end" className="min-w-56">
          <div className="flex items-center gap-2 px-2 py-1.5 text-sm">
            <span className="font-semibold">{login}</span>
            <RoleBadge role={role} />
          </div>
          <DropdownMenuSeparator />
          <DropdownMenuLabel>Theme</DropdownMenuLabel>
          <DropdownMenuRadioGroup value={choice} onValueChange={(v) => setChoice(v as ThemeChoice)}>
            {THEME_OPTIONS.map((o) => (
              // Keep the menu open so the effect of the change is visible.
              <DropdownMenuRadioItem key={o.value} value={o.value} onSelect={(e) => e.preventDefault()}>
                {o.label}
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
          <DropdownMenuSeparator />
          <DropdownMenuItem disabled={signingOut} onSelect={() => void signOut()}>
            Sign out
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </TooltipProvider>
  );
}

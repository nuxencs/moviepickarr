import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { LogOutIcon } from "lucide-react";

import { APIClient } from "@/api/APIClient";
import { Button } from "@/components/ui/button";
import { ModeToggle } from "@/components/ui/mode-toggle";
import { toast } from "@/components/ui/toast";
import type { AuthUser } from "@/types/Response";

interface HeaderProps {
  authUser: AuthUser;
}

export function Header({ authUser }: HeaderProps) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  const logoutMutation = useMutation({
    mutationFn: () => APIClient.auth.logout(),
    onSuccess: () => {
      queryClient.clear();
      toast.success("Logged out");
      void navigate({ to: "/login" });
    },
    onError: () => {
      toast.error("Logout failed");
    },
  });

  return (
    <div className="mt-4">
      <div className="p-4 flex items-center justify-between text-5xl font-semibold leading-none tracking-tight">
        Movie Gang
        <div className="flex items-center gap-2 text-lg font-semibold tracking-tight">
          <span className="hidden sm:inline">{authUser.name}</span>
          <Button
            variant="outline"
            size="icon"
            title="Logout"
            onClick={() => logoutMutation.mutate()}
            disabled={logoutMutation.isPending}
          >
            <LogOutIcon className="h-[1.2rem] w-[1.2rem]"/>
            <span className="sr-only">Logout</span>
          </Button>
          <ModeToggle/>
        </div>
      </div>
    </div>
  )
}

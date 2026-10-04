import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";

import { APIClient } from "@/api/APIClient";
import { clearPrincipalCache } from "@/api/principalCache";

import { apiMessage } from "@/components/moviepickarr/account/account";
import { toast } from "@/components/ui/toast-api";

/**
 * Ends this session (`all` false) or every session, then goes to /login. Clear
 * the principal cache before navigate: the next member on this browser must not
 * see the previous member's data. A failed logout keeps the cache: the session
 * may still be live.
 */
export function useLogout() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (all: boolean) => APIClient.auth.logout(all),
    onSuccess: async () => {
      await clearPrincipalCache(queryClient);
      void navigate({ to: "/login" });
    },
    onError: (err) => toast.error(apiMessage(err, "Couldn't log out.")),
  });
}

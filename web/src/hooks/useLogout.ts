import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";

import { APIClient } from "@/api/APIClient";
import { clearPrincipalCache } from "@/api/principalCache";

import { apiMessage } from "@/components/moviepickarr/account/account";
import { toast } from "@/components/ui/toast-api";

/**
 * The one way out of the app: end the session, drop the cached actor, land on
 * the login route. Two screens offer logout (the profile panel's quick exit
 * and the account page's sessions row, which also carries "log out
 * everywhere"), and they used to write the sequence out separately.
 *
 * `all` picks the scope: false ends this device's session, true ends every
 * session on the account.
 *
 * Clearing the principal cache before navigate is load-bearing, not stylistic.
 * The login page reads the cached actor, and principal-owned query or mutation
 * data must not survive into whoever signs in next on the same browser.
 *
 * A failed logout navigates nowhere and leaves the cache alone: the session
 * may well still be live, so the member stays where they are with the toast as
 * the only sign.
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

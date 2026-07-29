import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";

import { APIClient } from "@/api/APIClient";
import { AuthKeys } from "@/api/query_keys";

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
 * removeQueries before navigate is load-bearing, not stylistic. The login page
 * reads the cached actor, so leaving it in place for even a frame lets it
 * decide the member is still signed in and bounce them back into the app.
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
    onSuccess: () => {
      queryClient.removeQueries({ queryKey: AuthKeys.me() });
      void navigate({ to: "/login" });
    },
    onError: (err) => toast.error(apiMessage(err, "Couldn't log out.")),
  });
}

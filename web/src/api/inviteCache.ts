import { InvitesKeys, UsersKeys } from "@/api/query_keys";

import type { QueryClient } from "@tanstack/react-query";

/** Reconcile the invite generation and the roster credential projection as one
 * surface. Awaiting both keeps mutation busy states up until the rows agree. */
export function reconcileInviteSurfaces(queryClient: QueryClient) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: InvitesKeys.all }),
    queryClient.invalidateQueries({ queryKey: UsersKeys.roster() }),
  ]);
}

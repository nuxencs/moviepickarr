import { InvitesKeys, UsersKeys } from "@/api/query_keys";

import type { QueryClient } from "@tanstack/react-query";

/** Refetches invites and roster together; awaiting both keeps busy states up until they agree. */
export function reconcileInviteSurfaces(queryClient: QueryClient) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: InvitesKeys.all }),
    queryClient.invalidateQueries({ queryKey: UsersKeys.roster() }),
  ]);
}

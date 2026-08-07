import { useQuery } from "@tanstack/react-query";

import { MeQueryOptions } from "@/api/queries";
import { RadarrAttentionQueryOptions } from "@/api/radarr";

import { plural } from "@/components/moviepickarr/lib";

export function RadarrAttentionBadge() {
  const { data: me } = useQuery(MeQueryOptions());
  const attention = useQuery(RadarrAttentionQueryOptions(me?.role === "admin"));
  const count = attention.data?.count ?? 0;

  if (me?.role !== "admin" || count < 1) return null;

  return (
    <>
      <span className="admin-attention" aria-hidden="true">
        {count > 99 ? "99+" : count}
      </span>
      <span className="vis-hidden">
        , {plural(count, "Radarr acquisition")} {count === 1 ? "needs" : "need"} attention
      </span>
    </>
  );
}

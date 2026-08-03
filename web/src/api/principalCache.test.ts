import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";

import { clearPrincipalCache } from "@/api/principalCache";

describe("clearPrincipalCache", () => {
  it("cancels private work and removes query and mutation data", async () => {
    const client = new QueryClient();
    let aborted = false;
    const pending = client.fetchQuery({
      queryKey: ["private", "pending"],
      queryFn: ({ signal }) =>
        new Promise<string>((_resolve, reject) => {
          signal.addEventListener("abort", () => {
            aborted = true;
            reject(new Error("aborted"));
          });
        }),
    }).catch((error: unknown) => error);
    const mutation = client.getMutationCache().build<string, Error, void, unknown>(client, {
      mutationKey: ["private", "claim"],
      mutationFn: async () => "one-time-link",
    });
    await mutation.execute(undefined);

    await clearPrincipalCache(client);
    await pending;

    expect(aborted).toBe(true);
    expect(client.getQueryCache().getAll()).toHaveLength(0);
    expect(client.getMutationCache().getAll()).toHaveLength(0);
  });
});

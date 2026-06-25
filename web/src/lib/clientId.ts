/* A stable, anonymous per-browser id. Sent with a pick so the server can tag the
   pick with its initiator; only that client shows the reel's confirm button. It
   lives in localStorage so it survives reloads — the picker keeps the button (and
   the right to close the reel for everyone) even after refreshing mid-confirm. */

const KEY = "mp-client-id";

let cached: string | null = null;

export function getClientId(): string {
  if (typeof window === "undefined") return "";
  if (cached) return cached;
  let id = localStorage.getItem(KEY);
  if (!id) {
    id =
      typeof crypto !== "undefined" && "randomUUID" in crypto
        ? crypto.randomUUID()
        : `c-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
    localStorage.setItem(KEY, id);
  }
  cached = id;
  return id;
}

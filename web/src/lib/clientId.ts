/* Anonymous per-browser id sent with a draw; only the initiator shows the reel's
   confirm button. Kept in localStorage so a mid-confirm reload keeps that button. */

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

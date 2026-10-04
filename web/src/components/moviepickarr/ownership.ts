// The one adder-only ownership rule for the Members board. Render-only: the
// backend enforces it regardless.
export function isSelf(
  meID: number | undefined,
  memberID: number | undefined,
): boolean {
  // meID is undefined while /auth/me loads; a missing session never owns a board.
  return meID !== undefined && meID === memberID;
}

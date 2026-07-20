// The one ownership rule for the Members board: movie actions are adder-only,
// and the adder is always the session member. So every self-service gate on the
// board is the same question: is this member the session actor? Own-board
// actions compare against the board member's id; the watched-row edit compares
// against the movie's adder id. Both flow through here so the rule has one home
// (the backend enforces adder-only regardless; this only decides what to render).
export function isSelf(
  meID: number | undefined,
  memberID: number | undefined,
): boolean {
  // meID is undefined while /auth/me loads; a missing session never owns a board.
  return meID !== undefined && meID === memberID;
}

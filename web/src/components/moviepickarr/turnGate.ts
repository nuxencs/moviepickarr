// The next-up turn gate: who may run the draw → reveal → watch cycle. Mirrors
// the backend requireNextUpOrAdmin rule (an admin, or the member whose turn it
// is) so the board disables the three controls for everyone else instead of
// hiding them — the turn stays legible. The rule is a pure function of the
// session actor and the next-up member, unit-tested without rendering; the hook
// at the bottom wires it to the two queries.
import { useQuery } from "@tanstack/react-query";

import { MeQueryOptions, SettingsGetNextUpQueryOptions } from "@/api/queries";

import { possessive } from "@/components/moviepickarr/possessive";

export interface TurnGateInputs {
  /** The session actor's role, undefined while /auth/me is still loading. */
  role: "member" | "admin" | undefined;
  /** The session actor's member id, undefined while loading. */
  meID: number | undefined;
  /** The next-up member id; 0 (empty roster) or undefined (loading) means
   *  unresolved. */
  nextUpID: number | undefined;
  /** The next-up member's display name, "" when unresolved. */
  nextUpName: string | undefined;
}

export interface TurnGate {
  /** The viewer may act (an admin or the next-up member). Also true while the
   *  gate is still loading, so the controls stay live and the backend
   *  not_next_up is the backstop rather than a premature client-side lock. */
  canAct: boolean;
  /** Apply the disabled + tooltip treatment: the gate has resolved and the
   *  viewer is neither next-up nor admin. */
  locked: boolean;
  /** Next-up is a real member, not the empty-roster placeholder. Drives the
   *  named tooltip vs the waiting fallback. */
  resolved: boolean;
  /** The viewer *is* the next-up member (their turn right now). Narrower than
   *  `canAct`, which also covers admins and the loading window. Drives the
   *  "Your turn" vs "<name>'s turn" hero label, so an admin who isn't next-up
   *  still reads the turn-holder's name. */
  isSelf: boolean;
  /** The next-up member's name, "" when unresolved. */
  nextUpName: string;
}

/**
 * The turn rule. `canAct` errs open while either query is still loading (so the
 * real next-up member never flashes a locked control on first paint); once both
 * have resolved it is admin-or-next-up. `locked` is the inverse, but only once
 * the gate is known — never during the loading window.
 */
export function turnGate(input: TurnGateInputs): TurnGate {
  const ready = input.role !== undefined && input.nextUpID !== undefined;
  const isAdmin = input.role === "admin";
  const resolved = (input.nextUpID ?? 0) > 0;
  const isNextUp = resolved && input.meID !== undefined && input.meID === input.nextUpID;
  const canAct = !ready || isAdmin || isNextUp;
  return {
    canAct,
    locked: ready && !canAct,
    resolved,
    isSelf: isNextUp,
    nextUpName: input.nextUpName ?? "",
  };
}

/** Shown when next-up hasn't resolved to a member yet (empty roster). */
const WAITING_TIP = "Waiting for the next-up member.";

/** Tooltip for the disabled Draw control. */
export function drawLockedTip(gate: TurnGate): string {
  return gate.resolved ? `It's ${possessive(gate.nextUpName)} turn to draw.` : WAITING_TIP;
}

/** Tooltip for the disabled Reveal (OK) control. */
export function revealLockedTip(gate: TurnGate): string {
  return gate.resolved ? `Only ${gate.nextUpName} can reveal this draw.` : WAITING_TIP;
}

/** Tooltip for the disabled Mark-watched control. */
export function watchLockedTip(gate: TurnGate): string {
  return gate.resolved ? `Only ${gate.nextUpName} can mark this watched.` : WAITING_TIP;
}

/** Live turn gate, read from the session actor + next-up queries. */
export function useTurnGate(): TurnGate {
  const { data: me } = useQuery(MeQueryOptions());
  const { data: nextUp } = useQuery(SettingsGetNextUpQueryOptions());
  return turnGate({
    role: me?.role,
    meID: me?.id,
    nextUpID: nextUp?.id,
    nextUpName: nextUp?.name,
  });
}

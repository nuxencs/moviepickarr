// The next-up turn gate. Mirrors the backend requireNextUp rule (no admin exception);
// the board disables, not hides, the controls so the turn stays legible.
import { useQuery } from "@tanstack/react-query";

import { MeQueryOptions, SettingsGetNextUpQueryOptions } from "@/api/queries";

import { possessive } from "@/components/moviepickarr/possessive";

export interface TurnGateInputs {
  /** Undefined while /auth/me is loading. */
  role: "member" | "guest" | "admin" | undefined;
  meID: number | undefined;
  /** 0 (empty roster) or undefined (loading) means unresolved. */
  nextUpID: number | undefined;
  nextUpName: string | undefined;
}

export interface TurnGate {
  /** Also true while loading, so the backend not_next_up is the backstop, not a premature lock. */
  canAct: boolean;
  /** Resolved and not next-up: apply the disabled + tooltip treatment. */
  locked: boolean;
  /** Next-up is a real member, not the empty-roster placeholder. */
  resolved: boolean;
  /** The viewer is next-up; narrower than `canAct`, which also covers loading. */
  isSelf: boolean;
  /** Admin and a real member holds the turn. The skip control is hidden, not disabled, otherwise. */
  canSkip: boolean;
  guest: boolean;
  /** "" when unresolved. */
  nextUpName: string;
}

/** The turn rule. Errs open while loading, so the next-up member never sees a locked flash. */
export function turnGate(input: TurnGateInputs): TurnGate {
  const ready = input.role !== undefined && input.nextUpID !== undefined;
  const isAdmin = input.role === "admin";
  const guest = input.role === "guest";
  const resolved = (input.nextUpID ?? 0) > 0;
  const isNextUp = resolved && input.meID !== undefined && input.meID === input.nextUpID;
  const canAct = !guest && (!ready || isNextUp);
  return {
    canAct,
    locked: ready && !canAct,
    resolved,
    isSelf: isNextUp,
    canSkip: isAdmin && resolved,
    guest,
    nextUpName: input.nextUpName ?? "",
  };
}

const WAITING_TIP = "Waiting for the next-up member.";

export function drawLockedTip(gate: TurnGate): string {
  if (gate.guest) return "Guests can view the draw but cannot start one.";
  return gate.resolved ? `It's ${possessive(gate.nextUpName)} turn to draw.` : WAITING_TIP;
}

export function revealLockedTip(gate: TurnGate): string {
  if (gate.guest) return "Guests can view the draw but cannot reveal it.";
  return gate.resolved ? `Only ${gate.nextUpName} can reveal this draw.` : WAITING_TIP;
}

export function watchLockedTip(gate: TurnGate): string {
  if (gate.guest) return "Guests can view the draw but cannot mark it watched.";
  return gate.resolved ? `Only ${gate.nextUpName} can mark this watched.` : WAITING_TIP;
}

export const guestWildcardTip = "Guests can view Wildcards but cannot change them.";

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

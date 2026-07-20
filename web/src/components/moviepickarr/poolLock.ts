// The pool-lock gate: who may lock or unlock the shared pool. Locking is an
// admin-only action (the backend's handleSetPoolLock calls requireAdmin), so
// the Movies board disables the toggle for everyone else instead of hiding it,
// mirroring the turn gate's disable-not-hide treatment on the draw controls.
// The rule is a pure function of the session actor's role, unit-tested without
// rendering.

/**
 * Whether the session actor may toggle the pool lock. Errs open while /auth/me
 * is still loading (role undefined) so an admin never flashes a disabled toggle
 * on first paint; the backend requireAdmin is the backstop for a non-admin who
 * clicks during that window.
 */
export function canLockPool(role: "member" | "admin" | undefined): boolean {
  return role === undefined || role === "admin";
}

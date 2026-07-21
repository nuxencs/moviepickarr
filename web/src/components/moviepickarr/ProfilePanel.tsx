import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { LogOutIcon, MoonIcon, SettingsIcon, SunIcon } from "lucide-react";
import { useCallback, useEffect, useId, useRef } from "react";

import { APIClient } from "@/api/APIClient";
import { AuthKeys } from "@/api/query_keys";

import { apiMessage } from "@/components/moviepickarr/account/account";
import { Avatar } from "@/components/moviepickarr/Bits";
import { useTheme } from "@/components/theme-context";
import { toast } from "@/components/ui/toast-api";

import type { MeResponse } from "@/types/Response";

import { useDismissible } from "@/hooks/useDismissible";

/**
 * Resolve dark/light from the `theme` value directly (not the DOM class): the
 * class is applied by the parent ThemeProvider's effect, which runs after this
 * child's, so reading it here would be stale.
 */
function resolveDark(theme: string): boolean {
  if (theme === "system") {
    return window.matchMedia("(prefers-color-scheme: dark)").matches;
  }
  return theme === "dark";
}

/**
 * The top-right avatar and the profile panel it toggles: identity header, an
 * Account settings link, a Preferences section (theme today; the draw-sound
 * control moves in with #143), and a danger-styled single-device Log out.
 *
 * The panel rides the shared dismissal machine so its open/close motion and its
 * Escape / outside-click behaviour match the app's other popovers, and it's
 * anchored top-right and width-capped to the viewport so it never overflows on
 * phones (where the avatar sits in the same top bar).
 */
export function ProfilePanel({ me }: { me: MeResponse }) {
  const { theme, setTheme } = useTheme();
  const isDark = resolveDark(theme);
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const panelId = useId();

  const { open, closing, show, dismiss } = useDismissible({ restoreFocusTo: triggerRef });

  // Focus returns to the avatar on every dismissal except an outside click,
  // where focus follows the pointer instead.
  const requestClose = useCallback(
    (outside: boolean) => dismiss({ restoreFocus: !outside }),
    [dismiss],
  );

  // Dismiss on Escape or a click outside the trigger+panel root, matching the
  // menu / date-range popovers.
  useEffect(() => {
    if (!open || closing) return;
    const onPointerDown = (e: PointerEvent) => {
      if (rootRef.current?.contains(e.target as Node)) return;
      requestClose(true);
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        requestClose(false);
      }
    };
    document.addEventListener("pointerdown", onPointerDown, true);
    document.addEventListener("keydown", onKeyDown, true);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown, true);
      document.removeEventListener("keydown", onKeyDown, true);
    };
  }, [open, closing, requestClose]);

  // Single-device logout, mirroring the account page: end this session, drop the
  // cached actor so the login screen doesn't flash a stale "still signed in"
  // state, then land on the login route. "Log out everywhere" stays on the
  // account page.
  const logout = useMutation({
    mutationFn: () => APIClient.auth.logout(false),
    onSuccess: () => {
      queryClient.removeQueries({ queryKey: AuthKeys.me() });
      void navigate({ to: "/login" });
    },
    onError: (err) => toast.error(apiMessage(err, "Couldn't log out.")),
  });

  return (
    <div className="profile" ref={rootRef}>
      <button
        ref={triggerRef}
        type="button"
        className="profile__trigger"
        aria-label="Your profile"
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? panelId : undefined}
        onClick={() => (open && !closing ? requestClose(false) : show())}
      >
        <Avatar name={me.displayName} size={32} />
      </button>

      {open && (
        <div
          id={panelId}
          role="dialog"
          aria-label="Your profile"
          className={`profile__panel${closing ? " profile__panel--closing" : ""}`}
        >
          <div className="profile__id">
            <Avatar name={me.displayName} size={44} />
            <div className="profile__idtext">
              <div className="profile__idname">
                <span className="profile__idnametext">{me.displayName}</span>
                <span className="profile__tag">{me.role === "admin" ? "Admin" : "Member"}</span>
              </div>
              {me.username && <div className="profile__idsub">@{me.username}</div>}
            </div>
          </div>

          <div className="profile__sep" />

          <Link to="/settings" className="profile__item" onClick={() => requestClose(false)}>
            <SettingsIcon />
            Account settings
          </Link>

          <div className="profile__sep" />

          <div className="profile__section">
            <div className="profile__seclabel">Preferences</div>
            <div className="profile__pref">
              <span className="profile__prefname">Theme</span>
              <div className="seg" role="group" aria-label="Theme">
                <button type="button" data-active={!isDark} onClick={() => setTheme("light")}>
                  <SunIcon />
                  Light
                </button>
                <button type="button" data-active={isDark} onClick={() => setTheme("dark")}>
                  <MoonIcon />
                  Dark
                </button>
              </div>
            </div>
          </div>

          <div className="profile__sep" />

          <button
            type="button"
            className="profile__item profile__item--danger"
            onClick={() => logout.mutate()}
            disabled={logout.isPending}
          >
            <LogOutIcon />
            Log out
          </button>
        </div>
      )}
    </div>
  );
}

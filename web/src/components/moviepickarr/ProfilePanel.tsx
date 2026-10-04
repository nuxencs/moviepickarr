import { Link } from "@tanstack/react-router";
import { LogOutIcon, MoonIcon, SettingsIcon, SunIcon } from "lucide-react";
import { useCallback, useEffect, useId, useRef } from "react";

import { Avatar } from "@/components/moviepickarr/Bits";
import { VolumeControl } from "@/components/moviepickarr/VolumeControl";
import { useTheme } from "@/components/theme-context";

import type { MeResponse } from "@/types/Response";

import { useDismissible } from "@/hooks/useDismissible";
import { useLogout } from "@/hooks/useLogout";

// Not the DOM class: the parent ThemeProvider's effect sets it after this one runs.
function resolveDark(theme: string): boolean {
  if (theme === "system") {
    return window.matchMedia("(prefers-color-scheme: dark)").matches;
  }
  return theme === "dark";
}

/** The top-right avatar and the profile panel it toggles. */
export function ProfilePanel({ me }: { me: MeResponse }) {
  const { theme, setTheme } = useTheme();
  const isDark = resolveDark(theme);
  // Single-device only: "log out everywhere" stays on the account page.
  const logout = useLogout();

  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const panelId = useId();

  const { open, closing, show, dismiss, isTopmost } = useDismissible({ restoreFocusTo: triggerRef });

  const requestClose = useCallback(
    (outside: boolean) => dismiss({ restoreFocus: !outside }),
    [dismiss],
  );

  useEffect(() => {
    if (!open || closing) return;
    const onPointerDown = (e: PointerEvent) => {
      if (!isTopmost()) return;
      if (rootRef.current?.contains(e.target as Node)) return;
      requestClose(true);
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape" && isTopmost()) {
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
  }, [open, closing, requestClose, isTopmost]);

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
                <span className="profile__tag">
                  {me.role === "admin" ? "Admin" : me.role === "guest" ? "Guest" : "Member"}
                </span>
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
            <VolumeControl />
          </div>

          <div className="profile__sep" />

          <button
            type="button"
            className="profile__item profile__item--danger"
            onClick={() => logout.mutate(false)}
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

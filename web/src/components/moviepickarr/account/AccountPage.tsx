import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import {
  ChevronDownIcon,
  KeyRoundIcon,
  LinkIcon,
  LogOutIcon,
  MonitorSmartphoneIcon,
  RotateCcwIcon,
  UnlinkIcon,
} from "lucide-react";
import { useEffect, useRef, useState, type CSSProperties } from "react";

import { APIClient, ApiError, oidcLinkPath } from "@/api/APIClient";
import { reconcileInviteSurfaces } from "@/api/inviteCache";
import { AuthConfigQueryOptions, MeQueryOptions, SessionsQueryOptions } from "@/api/queries";
import { AuthKeys } from "@/api/query_keys";

import {
  apiMessage,
  linkResultFromSearch,
  PROVIDER,
  unlinkWouldStrand,
} from "@/components/moviepickarr/account/account";
import {
  ChangePasswordDialog,
  LogoutEverywhereDialog,
  SetPasswordDialog,
  UnlinkGuardDialog,
} from "@/components/moviepickarr/account/AccountOverlays";
import { DeviceIcon, SessionList } from "@/components/moviepickarr/account/SessionList";
import { otherDeviceCount, sessionMeta } from "@/components/moviepickarr/account/sessions";
import { Avatar } from "@/components/moviepickarr/Bits";
import { toast } from "@/components/ui/toast-api";

import type { SessionSummary } from "@/types/Response";

import { useLogout } from "@/hooks/useLogout";


import "@/components/moviepickarr/account/account.css";

// Only one ceremony is ever open, so a single tag drives the modal switch rather
// than a pile of booleans.
type Dialog = "change-password" | "set-password" | "logout-all" | "unlink-guard" | null;

export function AccountPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const me = useQuery(MeQueryOptions());
  const config = useQuery(AuthConfigQueryOptions());

  const [dialog, setDialog] = useState<Dialog>(null);
  // Server-side failure copy for the open credential dialog (a wrong current
  // password, a taken username), shown inline in the dialog rather than a toast.
  const [dialogError, setDialogError] = useState<string | null>(null);

  // useSearch keys off the route id (/_app/settings under the pathless layout);
  // useNavigate keys off the URL (/settings). Hence the two spellings.
  const { linked, error: oidcError } = useSearch({ from: "/_app/settings" });
  // Consume the OIDC-link redirect result exactly once, then strip the params so
  // a refresh or a back-nav doesn't re-toast. The ref keys on the raw params so
  // React's double-invoked dev effect fires the toast a single time.
  const handledLink = useRef<string | null>(null);
  useEffect(() => {
    const result = linkResultFromSearch(linked, oidcError);
    if (!result) return;
    const key = `${linked ?? ""}|${oidcError ?? ""}`;
    if (handledLink.current === key) return;
    handledLink.current = key;
    if (result.tone === "success") {
      toast.success(result.text);
      void queryClient.invalidateQueries({ queryKey: AuthKeys.me() });
      void reconcileInviteSurfaces(queryClient);
    } else {
      toast.error(result.text);
    }
    void navigate({ to: "/settings", search: {}, replace: true });
  }, [linked, oidcError, navigate, queryClient]);

  const openDialog = (kind: Dialog) => {
    setDialogError(null);
    setDialog(kind);
  };
  const closeDialog = () => {
    setDialog(null);
    setDialogError(null);
  };

  const refreshMe = () => queryClient.invalidateQueries({ queryKey: AuthKeys.me() });

  const changePassword = useMutation({
    mutationFn: ({ current, next }: { current: string; next: string }) =>
      APIClient.auth.changePassword(current, next),
    onSuccess: async () => {
      closeDialog();
      await Promise.all([
        refreshMe(),
        queryClient.invalidateQueries({ queryKey: AuthKeys.sessions() }),
      ]);
      toast.success("Password changed. Your other devices were signed out.");
    },
    onError: (err) => {
      // A wrong current password comes back as the uniform 401; everything else
      // is a genuine failure. Keep the dialog open with the reason inline.
      if (err instanceof ApiError && err.status === 401) {
        setDialogError("Your current password is incorrect.");
        return;
      }
      setDialogError(apiMessage(err, "Couldn't change your password."));
    },
  });

  const setPassword = useMutation({
    mutationFn: ({ username, password }: { username: string; password: string }) =>
      APIClient.auth.setPassword(username, password),
    onSuccess: () => {
      closeDialog();
      void refreshMe();
      toast.success("Password set. You can now log in with your username.");
    },
    onError: (err) => setDialogError(apiMessage(err, "Couldn't set your password.")),
    onSettled: () => reconcileInviteSurfaces(queryClient),
  });

  const unlinkSelf = useMutation({
    mutationFn: () => APIClient.members.unlinkSelf(),
    onSuccess: () => {
      void refreshMe();
      toast.success(`${PROVIDER} unlinked.`);
    },
    onError: (err) => {
      // The server 409s when this is the actor's only credential. If the client
      // guard was stale (a password removed in another tab), route that 409 into
      // the same "set a password first" dialog the guard shows, rather than a
      // bare toast, so the backstop gives the same guidance as the pre-check.
      if (err instanceof ApiError && err.status === 409) {
        openDialog("unlink-guard");
        return;
      }
      toast.error(apiMessage(err, `Couldn't unlink ${PROVIDER}.`));
    },
  });

  // Both sessions rows: false ends this device, true ends every session.
  const logout = useLogout();

  const sessions = useQuery(SessionsQueryOptions());
  const [revokingID, setRevokingID] = useState<string | null>(null);
  const revokeSession = useMutation({
    mutationFn: (s: SessionSummary) => APIClient.auth.revokeSession(s.id),
    onMutate: (s: SessionSummary) => setRevokingID(s.id),
    onSettled: () => setRevokingID(null),
    onSuccess: async (_data, s) => {
      await queryClient.invalidateQueries({ queryKey: AuthKeys.sessions() });
      toast.success(`Signed out of ${s.device}`);
    },
    onError: async (err, s) => {
      // A 404 means that device was already gone (swept, or signed out
      // elsewhere): the list the member clicked was stale, so refresh it and
      // say what happened rather than reporting a failure they can't act on.
      await queryClient.invalidateQueries({ queryKey: AuthKeys.sessions() });
      if (err instanceof ApiError && err.status === 404) {
        toast.success(`${s.device} was already signed out`);
        return;
      }
      toast.error(apiMessage(err, `Couldn't sign out of ${s.device}.`));
    },
  });

  const deviceList = sessions.data ?? [];
  const currentDevice = deviceList.find((session) => session.current);
  const remoteDevices = deviceList.filter((session) => !session.current);
  const otherDevices = otherDeviceCount(deviceList);
  const sessionActionsBusy = logout.isPending || revokeSession.isPending;

  if (me.isPending) {
    return <p className="acc-state">Loading your account…</p>;
  }
  if (!me.data) {
    // A 401 never reaches here: the _app route's beforeLoad redirects a
    // logged-out member to /login before the page renders. So this is a genuine
    // load failure (network, 5xx) rather than a missing session.
    return <p className="acc-state">Couldn&apos;t load your account. Try again in a moment.</p>;
  }

  const actor = me.data;
  const hasPassword = actor.hasLocalLogin;
  const hasSSO = actor.hasLinkedIdentity;
  const oidcConfigured = config.data?.oidc ?? false;
  const stranded = unlinkWouldStrand(hasPassword, hasSSO);

  const onUnlink = () => (stranded ? openDialog("unlink-guard") : unlinkSelf.mutate());

  return (
    <div className="acc">
      <header className="acc__head mg-rise" style={{ "--i": 0 } as CSSProperties}>
        <h1>Account</h1>
        <p>Manage your sign-in methods.</p>
      </header>

      {/* You — read-only identity. Naming is an admin concern; the username is
          stable, so there is no rename control here. */}
      <section className="acc__section mg-rise" style={{ "--i": 1 } as CSSProperties}>
        <h2 className="acc__label">You</h2>
        <div className="acc__identity">
          <Avatar name={actor.displayName} size={52} />
          <div className="acc__idtext">
            <div className="acc__idname">
              {actor.displayName}
              {actor.role === "admin" && <span className="acc-tag">Admin</span>}
            </div>
            <div className="acc__idsub">
              {actor.username ? `@${actor.username}` : `Signed in with ${PROVIDER}`}
            </div>
          </div>
        </div>
      </section>

      {/* Sign-in methods */}
      <section className="acc__section mg-rise" style={{ "--i": 2 } as CSSProperties}>
        <h2 className="acc__label">Sign-in</h2>

        {hasPassword ? (
          <div className="acc__row">
            <span className="acc__rowicon">
              <KeyRoundIcon />
            </span>
            <div className="acc__rowtext">
              <div className="acc__rowtitle">Password</div>
              <div className="acc__rowmeta">Used to sign in with your username.</div>
            </div>
            <button type="button" className="btn btn--ghost btn--sm" onClick={() => openDialog("change-password")}>
              <KeyRoundIcon aria-hidden="true" />
              Change
            </button>
          </div>
        ) : (
          <div className="acc__row acc__row--cta">
            <span className="acc__rowicon acc__rowicon--empty">
              <KeyRoundIcon />
            </span>
            <div className="acc__rowtext">
              <div className="acc__rowtitle">No password set</div>
              <div className="acc__rowmeta">You sign in with {PROVIDER}. Add a password as a backup way in.</div>
            </div>
            <button type="button" className="btn btn--accent btn--sm" onClick={() => openDialog("set-password")}>
              <KeyRoundIcon aria-hidden="true" />
              Set a password
            </button>
          </div>
        )}

        {/* SSO — the whole row is absent (not disabled) when no provider is
            configured, mirroring how the login SSO button is gone rather than
            greyed. */}
        {oidcConfigured &&
          (hasSSO ? (
            <div className="acc__row">
              <span className="acc__rowicon acc__rowicon--on">
                <LinkIcon />
              </span>
              <div className="acc__rowtext">
                <div className="acc__rowtitle">{PROVIDER}</div>
                <div className="acc__rowmeta">Connected</div>
              </div>
              <button
                type="button"
                className="btn btn--ghost btn--sm"
                onClick={onUnlink}
                disabled={unlinkSelf.isPending}
              >
                <UnlinkIcon />
                Unlink
              </button>
            </div>
          ) : (
            <div className="acc__row">
              <span className="acc__rowicon">
                <LinkIcon />
              </span>
              <div className="acc__rowtext">
                <div className="acc__rowtitle">{PROVIDER}</div>
                <div className="acc__rowmeta">Not connected</div>
              </div>
              <button
                type="button"
                className="btn btn--ghost btn--sm"
                onClick={() => window.location.assign(oidcLinkPath())}
              >
                <LinkIcon />
                Connect
              </button>
            </div>
          ))}
      </section>

      {/* The current device stays visible. Other devices are progressive
          disclosure: the count is enough for the common case, while the full
          revoke register appears only when the member chooses to manage it. */}
      <section className="acc__section mg-rise" style={{ "--i": 3 } as CSSProperties}>
        <h2 className="acc__label">Signed-in devices</h2>

        <div className="acc-devices">
          <div
            className="acc-device acc-device--current"
            aria-busy={logout.isPending || undefined}
          >
            <span className="acc-device__icon acc-device__icon--current">
              {currentDevice ? <DeviceIcon device={currentDevice.device} /> : <MonitorSmartphoneIcon />}
            </span>
            <div className="acc-device__text">
              <div className="acc-device__name">{currentDevice?.device ?? "This device"}</div>
              {currentDevice && <div className="acc-device__meta">{sessionMeta(currentDevice)}</div>}
            </div>
            <div className="acc-device__actions">
              <span className="acc-tag">This device</span>
              <button
                type="button"
                className="btn btn--ghost btn--sm"
                aria-label={logout.isPending ? "Logging out of this device" : "Log out this device"}
                onClick={() => logout.mutate(false)}
                disabled={sessionActionsBusy}
              >
                <LogOutIcon />
                {logout.isPending ? "Logging out…" : "Log out"}
              </button>
            </div>
          </div>

          {sessions.isPending && (
            <div className="acc-devices__status" role="status">Loading other devices…</div>
          )}
          {sessions.isError && (
            <div className="acc-devices__status" role="alert">
              <span>Couldn&apos;t load other devices.</span>
              <button type="button" className="btn btn--ghost btn--sm" onClick={() => void sessions.refetch()}>
                <RotateCcwIcon aria-hidden="true" />
                Retry
              </button>
              <button
                type="button"
                className="btn btn--ghost btn--sm"
                onClick={() => openDialog("logout-all")}
                disabled={sessionActionsBusy}
              >
                <LogOutIcon aria-hidden="true" />
                Log out everywhere
              </button>
            </div>
          )}

          {!sessions.isPending && !sessions.isError && otherDevices === 0 && (
            <div className="acc-devices__status">No other devices are signed in.</div>
          )}

          {!sessions.isPending && !sessions.isError && otherDevices > 0 && (
            <details className="acc-devices__others">
              <summary>
                <span className="acc-devices__summaryicon"><MonitorSmartphoneIcon /></span>
                <span>
                  <strong>{otherDevices === 1 ? "1 other device" : `${otherDevices} other devices`}</strong>
                  <small>Review or sign out devices you no longer use.</small>
                </span>
                <ChevronDownIcon className="acc-devices__chevron" />
              </summary>
              <SessionList
                sessions={remoteDevices}
                revokingID={revokingID}
                disabled={logout.isPending}
                onRevoke={(s) => revokeSession.mutate(s)}
              />
              <div className="acc-devices__footer">
                <span>Don&apos;t recognize a device?</span>
                <button
                  type="button"
                  className="btn btn--ghost btn--sm"
                  onClick={() => openDialog("logout-all")}
                  disabled={sessionActionsBusy}
                >
                  <LogOutIcon aria-hidden="true" />
                  Log out everywhere
                </button>
              </div>
            </details>
          )}
        </div>
      </section>

      <section className="acc__section mg-rise" style={{ "--i": 4 } as CSSProperties}>
        <h2 className="acc__label">Credits</h2>
        <div className="acc-credit">
          <a
            className="acc-credit__brand"
            href="https://www.themoviedb.org/"
            target="_blank"
            rel="noreferrer"
          >
            <img src="/tmdb-logo.svg" alt="TMDB" />
          </a>
          <p>This product uses the TMDB API but is not endorsed or certified by TMDB.</p>
        </div>
      </section>

      {dialog === "change-password" && (
        <ChangePasswordDialog
          pending={changePassword.isPending}
          serverError={dialogError}
          onSubmit={(current, next) => changePassword.mutate({ current, next })}
          onClose={closeDialog}
        />
      )}
      {dialog === "set-password" && (
        <SetPasswordDialog
          pending={setPassword.isPending}
          serverError={dialogError}
          onSubmit={(username, password) => setPassword.mutate({ username, password })}
          onClose={closeDialog}
        />
      )}
      {dialog === "logout-all" && (
        <LogoutEverywhereDialog
          otherSessions={otherDevices}
          pending={logout.isPending}
          onConfirm={() => logout.mutate(true)}
          onClose={closeDialog}
        />
      )}
      {dialog === "unlink-guard" && (
        <UnlinkGuardDialog onSetPassword={() => openDialog("set-password")} onClose={closeDialog} />
      )}
    </div>
  );
}

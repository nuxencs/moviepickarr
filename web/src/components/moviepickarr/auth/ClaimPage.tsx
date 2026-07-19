import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "@tanstack/react-router";
import { CheckCircle2Icon, KeyRoundIcon, LockIcon, TriangleAlertIcon, UserIcon } from "lucide-react";
import { useState } from "react";

import { APIClient, oidcClaimPath } from "@/api/APIClient";
import { ClaimQueryOptions } from "@/api/queries";
import { AuthKeys } from "@/api/query_keys";

import { AuthFrame } from "@/components/moviepickarr/auth/AuthFrame";
import { claimTerminalFromError, validateClaimForm } from "@/components/moviepickarr/auth/authScreens";

import type { ClaimInfo } from "@/types/Response";

// The collapsed no-longer-valid state (expired / revoked / unknown token). A
// dead end: the only recovery is an admin issuing a fresh invite out of band.
function InvalidScreen() {
  return (
    <div className="auth__form auth__msg">
      <span className="auth__msgicon" data-tone="error">
        <TriangleAlertIcon />
      </span>
      <h1 className="auth__title">This invite is done</h1>
      <p className="auth__lead">
        The link expired or was already used. An admin can send you a fresh invite.
      </p>
    </div>
  );
}

// A transient failure (server error, dropped connection) — distinct from the
// dead-invite state so we never tell a member their invite is gone when it's
// really just a hiccup. Offers a retry rather than sending them to an admin.
function ErrorScreen({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="auth__form auth__msg">
      <span className="auth__msgicon" data-tone="error">
        <TriangleAlertIcon />
      </span>
      <h1 className="auth__title">Something went wrong</h1>
      <p className="auth__lead">
        We couldn&rsquo;t load your invite just now. Check your connection and try again.
      </p>
      <button type="button" className="btn btn--accent auth__submit" onClick={onRetry}>
        Try again
      </button>
    </div>
  );
}

// The distinct already-set-up state: this member can already log in, so point
// them at the login page rather than leaving them on a dead claim form.
function AlreadyScreen({ onGoToLogin }: { onGoToLogin: () => void }) {
  return (
    <div className="auth__form auth__msg">
      <span className="auth__msgicon" data-tone="ok">
        <CheckCircle2Icon />
      </span>
      <h1 className="auth__title">You&rsquo;re already in</h1>
      <p className="auth__lead">This login is already set up. Just sign in.</p>
      <button type="button" className="btn btn--accent auth__submit" onClick={onGoToLogin}>
        Go to login
      </button>
    </div>
  );
}

function ClaimForm({ token, claim }: { token: string; claim: ClaimInfo }) {
  const navigate = useNavigate({ from: "/claim/$token" });
  const queryClient = useQueryClient();
  const reset = claim.mode === "reset";

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [formError, setFormError] = useState<string | null>(null);

  const submit = useMutation({
    mutationFn: () =>
      APIClient.auth.claimPassword(token, password, reset ? undefined : username.trim()),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: AuthKeys.me() });
      void navigate({ to: "/" });
    },
  });

  const onSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const problem = validateClaimForm({ mode: claim.mode, username, password, confirm });
    if (problem) {
      setFormError(problem);
      return;
    }
    setFormError(null);
    submit.mutate();
  };

  // Local validation wins the error slot; otherwise surface a submit failure
  // (most often an invite consumed or expired between load and submit).
  const error = formError
    ? formError
    : submit.error
      ? "Couldn't finish setting up your login. The invite may no longer be valid; ask an admin for a fresh one."
      : null;

  return (
    <form className="auth__form" onSubmit={onSubmit}>
      <div className="auth__eyebrow">{reset ? "Password reset" : "Invited by an admin"}</div>
      <h1 className="auth__title">
        {reset ? "Choose a new password" : `Welcome, ${claim.displayName}`}
      </h1>
      <p className="auth__lead">
        {reset
          ? `Set a new password, ${claim.displayName}, and you're back in.`
          : "Set a password to finish joining the roster."}
      </p>

      {!reset && (
        <label className="field auth__field">
          <UserIcon />
          <input
            aria-label="Username"
            placeholder="Username"
            autoComplete="username"
            autoFocus
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
        </label>
      )}
      <label className="field auth__field">
        <LockIcon />
        <input
          aria-label={reset ? "New password" : "Password"}
          type="password"
          placeholder={reset ? "New password" : "Password"}
          autoComplete="new-password"
          autoFocus={reset}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
      </label>
      <label className="field auth__field">
        <LockIcon />
        <input
          aria-label="Confirm password"
          type="password"
          placeholder="Confirm password"
          autoComplete="new-password"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
        />
      </label>

      {error && <p className="auth__error">{error}</p>}

      <button
        type="submit"
        className="btn btn--accent auth__submit"
        disabled={submit.isPending}
      >
        {reset ? "Update password" : "Create login"}
      </button>

      {!reset && claim.options.oidc && (
        <button
          type="button"
          className="btn btn--ghost auth__submit"
          onClick={() => window.location.assign(oidcClaimPath(token))}
        >
          <KeyRoundIcon />
          Set up with SSO instead
        </button>
      )}
    </form>
  );
}

export function ClaimPage() {
  const { token } = useParams({ from: "/claim/$token" });
  const navigate = useNavigate({ from: "/claim/$token" });
  const claim = useQuery(ClaimQueryOptions(token));

  if (claim.isLoading) {
    return (
      <AuthFrame>
        <p className="auth__lead">Checking your invite&hellip;</p>
      </AuthFrame>
    );
  }

  if (claim.error) {
    // 404 → no longer valid; 410 → already set up; anything else (5xx, network)
    // is a transient error the member can retry, not a dead invite.
    const terminal = claimTerminalFromError(claim.error);
    return (
      <AuthFrame>
        {terminal === "already" ? (
          <AlreadyScreen onGoToLogin={() => void navigate({ to: "/login" })} />
        ) : terminal === "invalid" ? (
          <InvalidScreen />
        ) : (
          <ErrorScreen onRetry={() => void claim.refetch()} />
        )}
      </AuthFrame>
    );
  }

  if (!claim.data) {
    return (
      <AuthFrame>
        <InvalidScreen />
      </AuthFrame>
    );
  }

  return (
    <AuthFrame>
      <ClaimForm token={token} claim={claim.data} />
    </AuthFrame>
  );
}

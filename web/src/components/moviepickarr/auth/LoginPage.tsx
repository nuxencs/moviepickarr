import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { LockIcon, TriangleAlertIcon, UserIcon } from "lucide-react";
import { useState } from "react";

import { APIClient, oidcLoginPath } from "@/api/APIClient";
import { clearPrincipalCache } from "@/api/principalCache";
import { AuthConfigQueryOptions } from "@/api/queries";

import { AuthFrame } from "@/components/moviepickarr/auth/AuthFrame";
import {
  bannerForLoginError,
  bannerForOidcError,
  type Banner,
} from "@/components/moviepickarr/auth/authScreens";

function BannerRow({ banner }: { banner: Banner | null }) {
  if (!banner) return null;
  return (
    <div className="auth__note" data-tone={banner.tone} role="alert">
      <TriangleAlertIcon />
      <span>{banner.text}</span>
    </div>
  );
}

export function LoginPage() {
  const navigate = useNavigate({ from: "/login" });
  const queryClient = useQueryClient();
  // The OIDC callback lands back here with a ?error= bucket on failure.
  const { error: oidcError } = useSearch({ from: "/login" });

  const config = useQuery(AuthConfigQueryOptions());

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");

  // The already-signed-in bounce lives in the route's beforeLoad (see router.tsx):
  // resolving /me before render means a live session never paints the form.

  const login = useMutation({
    mutationFn: () => APIClient.auth.login(username.trim(), password),
    onSuccess: async () => {
      await clearPrincipalCache(queryClient);
      void navigate({ to: "/" });
    },
  });

  // A submit failure wins the banner slot (it is the member's most recent
  // action); otherwise show the OIDC redirect notice, if any.
  const banner: Banner | null = login.error
    ? bannerForLoginError(login.error)
    : bannerForOidcError(oidcError);

  return (
    <AuthFrame>
      <form
        className="auth__form"
        onSubmit={(e) => {
          e.preventDefault();
          login.mutate();
        }}
      >
        <div className="auth__eyebrow">Welcome back</div>
        <h1 className="auth__title">Sign in to movie night</h1>
        <BannerRow banner={banner} />
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
        <label className="field auth__field">
          <LockIcon />
          <input
            aria-label="Password"
            type="password"
            placeholder="Password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </label>
        <button
          type="submit"
          className="btn btn--accent auth__submit"
          disabled={login.isPending || !username.trim() || !password}
        >
          Sign in
        </button>
        {config.data?.oidc && (
          <button
            type="button"
            className="btn btn--ghost auth__submit"
            onClick={() => window.location.assign(oidcLoginPath())}
          >
            Log in with SSO
          </button>
        )}
      </form>
    </AuthFrame>
  );
}

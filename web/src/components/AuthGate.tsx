import { useMutation } from "@tanstack/react-query";
import { FormEvent, useState } from "react";

import { APIClient } from "@/api/APIClient";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { toast } from "@/components/ui/toast";
import type { AuthUser } from "@/types/Response";

interface AuthGateProps {
  onAuthenticated: (authUser: AuthUser) => void;
}

export function AuthGate({ onAuthenticated }: AuthGateProps) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [loginError, setLoginError] = useState("");

  const loginMutation = useMutation({
    mutationFn: (e: FormEvent) => {
      e.preventDefault();
      return APIClient.auth.login(username, password);
    },
    onSuccess: (data) => {
      setLoginError("");
      toast.success(`Welcome ${data.name}`);
      onAuthenticated(data);
      setPassword("");
    },
    onError: (error) => {
      const message = error instanceof Error && error.message.toLowerCase().includes("unauthorized")
        ? "Invalid username or password."
        : "Login failed. Please try again.";
      setLoginError(message);
    },
  });

  const isPending = loginMutation.isPending;
  const canSubmit = username.trim().length > 0 && password.trim().length > 0;

  return (
    <div className="min-h-screen flex items-center justify-center p-4">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>Sign In</CardTitle>
        </CardHeader>
        <CardContent>
          <form className="space-y-3" onSubmit={loginMutation.mutate}>
            <Input
              type="text"
              value={username}
              onChange={(e) => {
                setUsername(e.target.value);
                if (loginError) {
                  setLoginError("");
                }
              }}
              placeholder="Username"
              disabled={isPending}
              required
            />
            <Input
              type="password"
              value={password}
              onChange={(e) => {
                setPassword(e.target.value);
                if (loginError) {
                  setLoginError("");
                }
              }}
              placeholder="Password"
              disabled={isPending}
              required
            />
            {loginError ? (
              <p className="text-sm text-red-500">{loginError}</p>
            ) : null}
            <Button type="submit" className="w-full" disabled={isPending || !canSubmit}>
              Login
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}

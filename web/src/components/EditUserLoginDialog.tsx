import { useEffect, useState } from "react";

import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

interface EditUserLoginDialogSubmit {
  username: string;
  password: string;
  role: "member" | "admin";
}

interface EditUserLoginDialogProps {
  isOpen: boolean;
  onClose: () => void;
  displayName: string;
  initialUsername: string;
  initialRole: "member" | "admin";
  hasAccount: boolean;
  isSaving?: boolean;
  onSubmit: (payload: EditUserLoginDialogSubmit) => void;
}

export function EditUserLoginDialog({
  isOpen,
  onClose,
  displayName,
  initialUsername,
  initialRole,
  hasAccount,
  isSaving = false,
  onSubmit,
}: EditUserLoginDialogProps) {
  const [username, setUsername] = useState(initialUsername);
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<"member" | "admin">(initialRole);

  useEffect(() => {
    if (!isOpen) {
      return;
    }

    setUsername(initialUsername);
    setPassword("");
    setRole(initialRole);
  }, [initialRole, initialUsername, isOpen]);

  const usernameValue = username.trim();
  const passwordValue = password.trim();
  const isDisabled = isSaving || usernameValue.length === 0 || passwordValue.length === 0;

  return (
    <AlertDialog open={isOpen} onOpenChange={onClose}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{hasAccount ? "Reset Login" : "Create Login"}</AlertDialogTitle>
          <AlertDialogDescription>
            {displayName}
          </AlertDialogDescription>
        </AlertDialogHeader>

        <div className="space-y-3">
          <Input
            type="text"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder="Username"
            disabled={isSaving}
          />
          <Input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder={hasAccount ? "New password" : "Temporary password"}
            disabled={isSaving}
          />
          <select
            value={role}
            onChange={(e) => setRole(e.target.value as "member" | "admin")}
            disabled={isSaving}
            className="h-9 w-full rounded-md border bg-background px-3 text-sm"
          >
            <option value="member">Member</option>
            <option value="admin">Admin</option>
          </select>
        </div>

        <AlertDialogFooter>
          <AlertDialogCancel disabled={isSaving}>Cancel</AlertDialogCancel>
          <Button
            disabled={isDisabled}
            onClick={() => onSubmit({ username: usernameValue, password: passwordValue, role })}
          >
            Save
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

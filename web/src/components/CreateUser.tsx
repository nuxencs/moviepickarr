import { APIClient } from "@/api/APIClient";
import { UsersKeys } from "@/api/query_keys";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { toast } from "@/components/ui/toast";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { PlusIcon } from "lucide-react";
import { FormEvent, useState } from "react";

interface CreateUserProps {
  onCreated?: () => void;
}

export function CreateUser({ onCreated }: CreateUserProps) {
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<"member" | "admin">("member");

  const createMutation = useMutation({
    mutationFn: (e: FormEvent) => {
      e.preventDefault();
      return APIClient.users.create(name, username, password, role)
    },
    onSuccess: () => {
      toast.success(`User ${name} created successfully!`);
      setName('');
      setUsername('');
      setPassword('');
      setRole("member");
      void queryClient.invalidateQueries({ queryKey: UsersKeys.list() });
      onCreated?.();
    },
    onError: () => {
      toast.error("Error creating user");
      setName('');
      setUsername('');
      setPassword('');
    }
  })

  return (
    <form className="space-y-2" onSubmit={createMutation.mutate}>
      <Input
        type="text"
        value={name}
        onChange={(e) => setName(e.target.value)}
        placeholder="Display name"
        disabled={createMutation.isPending}
        required
      />
      <Input
        type="text"
        value={username}
        onChange={(e) => setUsername(e.target.value)}
        placeholder="Login username"
        disabled={createMutation.isPending}
        required
      />
      <Input
        type="password"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        placeholder="Temporary password"
        disabled={createMutation.isPending}
        required
      />
      <select
        value={role}
        onChange={(e) => setRole(e.target.value as "member" | "admin")}
        disabled={createMutation.isPending}
        className="h-9 w-full rounded-md border bg-background px-3 text-sm"
      >
        <option value="member">Member</option>
        <option value="admin">Admin</option>
      </select>
      <Button
        type="submit"
        disabled={createMutation.isPending || name.length === 0 || username.length === 0 || password.length === 0}
        className="w-full"
      >
        <PlusIcon/>
        {createMutation.isPending ? 'Adding...' : 'Add User'}
      </Button>
    </form>
  );
}

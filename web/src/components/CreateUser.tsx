import { useMutation, useQueryClient } from "@tanstack/react-query";
import { PlusIcon } from "lucide-react";
import { FormEvent, useState } from "react";

import { APIClient } from "@/api/APIClient";
import { UsersKeys } from "@/api/query_keys";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { toast } from "@/components/ui/toast";

export function CreateUser() {
    const queryClient = useQueryClient();
    const [username, setUsername] = useState("");

    const createMutation = useMutation({
        mutationFn: (e: FormEvent) => {
            e.preventDefault();
            return APIClient.users.create(username)
        },
        onSuccess: () => {
            toast.success(`User ${username} created successfully!`);
            setUsername('');
            void queryClient.invalidateQueries({ queryKey: UsersKeys.list() });
        },
        onError: () => {
            toast.error("Error creating user");
            setUsername('');
        }
    })

    return (
        <form className="flex gap-2" onSubmit={createMutation.mutate}>
            <Input
                type="text"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="Enter username"
                disabled={createMutation.isPending}
                required
            />
            <Button
                type="submit"
                disabled={createMutation.isPending || username.length === 0}
            >
                <PlusIcon />
                {createMutation.isPending ? 'Adding...' : 'Add User'}
            </Button>
        </form>
    );
}

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDownIcon, ChevronUpIcon, PencilIcon, Trash2Icon, UserIcon } from "lucide-react";
import { useState } from "react";

import { APIClient } from "@/api/APIClient";
import { UsersGetAllQueryOptions } from "@/api/queries";
import { UsersKeys } from "@/api/query_keys";

import { CreateUser } from "@/components/CreateUser";
import { EditUserLoginDialog } from "@/components/EditUserLoginDialog";
import { UserMovies } from "@/components/UserMovies";
import { AnimatedListItem } from "@/components/ui/animated-list";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { DeletionDialog } from "@/components/ui/deletion-dialog";
import { toast } from "@/components/ui/toast";

import { useToggle } from "@/hooks/hooks";
import type { AuthUser, User } from "@/types/Response";

interface UserItemProps {
  authUser: AuthUser;
  user: User;
  canDelete: boolean;
}

function UserItem({ authUser, user, canDelete }: UserItemProps) {
  const queryClient = useQueryClient();
  const [deleteModalIsOpen, toggleDeleteModal] = useToggle(false);
  const [editLoginDialogIsOpen, toggleEditLoginDialog] = useToggle(false);

  const deleteMutation = useMutation({
    mutationFn: (userID: number) => APIClient.users.delete(userID),
    onSuccess: () => {
      toast.success(`User ${user.name} deleted successfully!`);
      void queryClient.invalidateQueries({ queryKey: UsersKeys.list() });
    },
    onError: () => {
      toast.error("Error deleting user");
    },
  });

  const accountMutation = useMutation({
    mutationFn: (payload: { username: string; password: string; role: "member" | "admin" }) =>
      APIClient.users.upsertAccount(user.userID, payload.username, payload.password, payload.role),
    onSuccess: () => {
      toast.success(`Login updated for ${user.name}`);
      void queryClient.invalidateQueries({ queryKey: UsersKeys.list() });
      toggleEditLoginDialog();
    },
    onError: () => {
      toast.error("Failed to update login");
    },
  });

  return (
    <>
      <EditUserLoginDialog
        isOpen={editLoginDialogIsOpen}
        onClose={toggleEditLoginDialog}
        displayName={user.name}
        initialUsername={user.username || user.name.toLowerCase().replace(/\s+/g, "_")}
        initialRole={user.role || "member"}
        hasAccount={user.hasAccount}
        isSaving={accountMutation.isPending}
        onSubmit={(payload) => accountMutation.mutate(payload)}
      />

      <DeletionDialog
        isOpen={deleteModalIsOpen}
        onClose={toggleDeleteModal}
        onConfirm={() => deleteMutation.mutate(user.userID)}
        title="Delete User"
        description={`Are you sure you want to delete ${user.name}? This action cannot be undone.`}
        confirmText="Delete"
        cancelText="Cancel"
      />

      <AnimatedListItem id={user.userID}>
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <UserIcon className="size-5"/> {user.name}
              </div>
              {canDelete ? (
                <div className="flex items-center gap-1">
                  <Button
                    variant="outline"
                    size="icon"
                    onClick={toggleEditLoginDialog}
                    title={user.hasAccount ? "Reset login" : "Create login"}
                  >
                    <PencilIcon className="size-4"/>
                    <span className="sr-only">{user.hasAccount ? "Reset login" : "Create login"}</span>
                  </Button>
                  <Button
                    variant="destructive"
                    size="icon"
                    onClick={toggleDeleteModal}
                  >
                    <Trash2Icon/>
                  </Button>
                </div>
              ) : null}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <UserMovies authUser={authUser} user={user}/>
          </CardContent>
        </Card>
      </AnimatedListItem>
    </>
  );
}

interface UsersGridProps {
  authUser: AuthUser;
}

export function UsersGrid({ authUser }: UsersGridProps) {
  const [createOpen, setCreateOpen] = useState(false);
  const { data: users } = useQuery(UsersGetAllQueryOptions());
  const visibleUsers = users ?? [];

  return (
    <div className="p-4 pt-0">
      {authUser.role === "admin" && (
        <Card className="w-full">
          <CardHeader>
            <CardTitle className="flex items-center justify-between">
              User Management
              <Button
                variant="outline"
                size="sm"
                onClick={() => setCreateOpen((current) => !current)}
              >
                {createOpen ? <ChevronUpIcon className="size-4"/> : <ChevronDownIcon className="size-4"/>}
                {createOpen ? "Hide" : "Add User"}
              </Button>
            </CardTitle>
          </CardHeader>
          {createOpen ? (
            <CardContent>
              <CreateUser onCreated={() => setCreateOpen(false)}/>
            </CardContent>
          ) : null}
        </Card>
      )}

      <div className="mt-4">
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {visibleUsers.map((user) => (
            <UserItem
              key={user.userID}
              authUser={authUser}
              user={user}
              canDelete={authUser.role === "admin"}
            />
          ))}
        </div>
      </div>
    </div>
  );
}

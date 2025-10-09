import { APIClient } from "@/api/APIClient";
import { UsersGetAllQueryOptions } from "@/api/queries";

import { CreateUser } from "@/components/CreateUser";
import { AnimatedListItem } from "@/components/ui/animated-list";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { DeletionDialog } from "@/components/ui/deletion-dialog";
import { toast } from "@/components/ui/toast";
import { UserMovies } from "@/components/UserMovies";

import { useToggle } from "@/hooks/hooks";
import { User } from "@/types/Response";

import { useMutation, useQuery } from "@tanstack/react-query";
import { Trash2Icon, UserIcon } from "lucide-react";

interface UserItemProps {
  user: User
}

function UserItem({ user }: UserItemProps) {
  const [deleteModalIsOpen, toggleDeleteModal] = useToggle(false);

  const deleteMutation = useMutation({
    mutationFn: (userID: string) => APIClient.users.delete(userID),
    onSuccess: () => {
      toast.success(`User ${user.name} deleted successfully!`);
    },
    onError: () => {
      toast.error("Error deleting user");
    }
  })

  return (
    <>
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
              <Button
                variant="destructive"
                size="icon"
                onClick={toggleDeleteModal}
              >
                <Trash2Icon/>
              </Button>
            </CardTitle>
          </CardHeader>
          <CardContent>
            <UserMovies user={user}/>
          </CardContent>
        </Card>
      </AnimatedListItem>
    </>
  )
}

export function UsersGrid() {
  const { data: users } = useQuery(UsersGetAllQueryOptions());

  return (
    <div className="p-4 pt-0">
      <Card className="w-full">
        <CardHeader>
          <CardTitle>User Management</CardTitle>
        </CardHeader>
        <CardContent>
          <CreateUser/>
        </CardContent>
      </Card>

      <div className="mt-4">
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {users?.map((user) => (
            <UserItem key={user.userID} user={user}/>
          ))}
        </div>
      </div>
    </div>
  )
}

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Trash2Icon, UserIcon } from "lucide-react";

import { APIClient } from "@/api/APIClient";
import { UsersGetAllQueryOptions } from "@/api/queries";
import { MoviesKeys, UsersKeys } from "@/api/query_keys";

import { CreateUser } from "@/components/CreateUser";
import { UserMovies } from "@/components/UserMovies";

import { AnimatedListItem } from "@/components/ui/animated-list";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { DeletionDialog } from "@/components/ui/deletion-dialog";
import { toast } from "@/components/ui/toast";

import { useToggle } from "@/hooks/hooks";
import { User } from "@/types/Response";

interface UserItemProps {
    user: User
}

function UserItem({ user }: UserItemProps) {
    const queryClient = useQueryClient();
    const [deleteModalIsOpen, toggleDeleteModal] = useToggle(false);

    const deleteMutation = useMutation({
        mutationFn: (userID: string) => APIClient.users.delete(userID),
        onSuccess: () => {
            toast.success(`User ${user.name} deleted successfully!`);
            void queryClient.invalidateQueries({ queryKey: UsersKeys.list() });
            void queryClient.invalidateQueries({ queryKey: MoviesKeys.listpool() });
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
            <AnimatedListItem key={user.userID} id={user.userID}>
                <Card>
                    <CardHeader>
                        <CardTitle className="flex items-center justify-between">
                            <div className="flex items-center gap-2">
                                <UserIcon className="w-5 h-5" />
                                <span>{user.name}</span>
                            </div>
                            <Button
                                variant="destructive"
                                size="icon"
                                onClick={toggleDeleteModal}
                            >
                                <Trash2Icon />
                            </Button>
                        </CardTitle>
                    </CardHeader>
                    <CardContent>
                        <UserMovies user={user} />
                    </CardContent>
                </Card>
            </AnimatedListItem>
        </>
    )
}

export function UsersGrid() {
    const { data: users } = useQuery(UsersGetAllQueryOptions());

    return (
        <div className="p-4">
            <Card className="w-full">
                <CardHeader>
                    <CardTitle>User Management</CardTitle>
                </CardHeader>
                <CardContent>
                    <CreateUser />
                </CardContent>
            </Card>

            <div className="mt-4">
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                    {users?.map((user) => (
                        <UserItem user={user} />
                    ))}
                </div>
            </div>
        </div>
    )
}

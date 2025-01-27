import {useState} from "react";
import CreateUser from "./CreateUser";
import {UserMovies} from "./UserMovies";
import {Trash2, User} from "lucide-react";
import {Button} from "@/components/ui/button";
import {ConfirmDialog} from "@/components/ui/confirm-dialog";
import {AnimatedListItem} from "@/components/ui/animated-list";
import {Card, CardContent, CardHeader, CardTitle} from "@/components/ui/card";
import {User as UserData} from "@/types/Response";
import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query";
import {APIClient} from "@/api/APIClient";
import {UsersGetAllQueryOptions} from "@/api/queries";
import {MoviesKeys, UsersKeys} from "@/api/query_keys";
import {toast} from "@/components/ui/toast";

export function Users() {
    const queryClient = useQueryClient();
    const [userToDelete, setUserToDelete] = useState<UserData | null>(null);

    const userQuery = useQuery(UsersGetAllQueryOptions());

    const deleteMutation = useMutation({
        mutationFn: (user: UserData) => APIClient.users.delete(user.userID),
        onSuccess: () => {
            toast.success(`User ${deleteMutation.variables?.name} deleted successfully!`);
            void queryClient.invalidateQueries({queryKey: UsersKeys.list()});
            void queryClient.invalidateQueries({queryKey: MoviesKeys.listpool()});
        },
        onError: () => {
            toast.error("Error deleting user");
        }
    })

    const onConfirm = () => {
        if (userToDelete) {
            deleteMutation.mutate(userToDelete)
        }
    }

    return (
        <div className="p-4">
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
                    {userQuery.data?.map((user) => (
                        <AnimatedListItem key={user.userID} id={user.userID}>
                            <Card>
                                <CardHeader>
                                    <CardTitle className="flex items-center justify-between">
                                        <div className="flex items-center gap-2">
                                            <User className="w-5 h-5"/>
                                            <span>{user.name}</span>
                                        </div>
                                        <Button
                                            variant="destructive"
                                            size="icon"
                                            onClick={() => setUserToDelete(user)}
                                        >
                                            <Trash2/>
                                        </Button>
                                    </CardTitle>
                                </CardHeader>
                                <CardContent>
                                    <UserMovies user={user}/>
                                </CardContent>
                            </Card>
                        </AnimatedListItem>
                    ))}
                </div>

                <ConfirmDialog
                    isOpen={!!userToDelete}
                    onClose={() => setUserToDelete(null)}
                    onConfirm={onConfirm}
                    title="Delete User"
                    description={`Are you sure you want to delete ${userToDelete?.name}? This action cannot be undone.`}
                    confirmText="Delete"
                    cancelText="Cancel"
                />
            </div>
        </div>
    )
}

import React from "react";
import CreateUser from "./CreateUser";
import UserMovies from "./UserMovies";
import {Trash2, User} from "lucide-react";
import {Button} from "@/components/ui/button";
import {ConfirmDialog} from "@/components/ui/confirm-dialog";
import {AnimatedListItem} from "@/components/ui/animated-list";
import {Card, CardContent, CardHeader, CardTitle} from "@/components/ui/card";
import {Movie, User as UserData} from "@/types/Response";

interface UserManagementProps {
    users: UserData[];
    userIDIsDeleting: string | null;
    userToDelete: UserData | null;
    onUserCreate: (user: UserData) => void;
    onInitiateUserDelete: (user: UserData) => void;
    onUserSetDeleting: (user: UserData | null) => void;
    onUserDelete: () => void;
    onMovieAdd: (movie: Movie) => void;
    onMovieDelete: (userID: string, movieID: string) => void;
    onMovieMove: (userID: string, movieID: string) => void;
}

const UserManagement: React.FC<UserManagementProps> = ({
                                                           users,
                                                           userIDIsDeleting,
                                                           userToDelete,
                                                           onUserCreate,
                                                           onInitiateUserDelete,
                                                           onUserSetDeleting,
                                                           onUserDelete,
                                                           onMovieAdd,
                                                           onMovieDelete,
                                                           onMovieMove,

                                                       }) => {
    return (
        <div className="p-4">
            <Card className="w-full">
                <CardHeader>
                    <CardTitle>User Management</CardTitle>
                </CardHeader>
                <CardContent>
                    <CreateUser onUserCreated={onUserCreate}/>
                </CardContent>
            </Card>

            <div className="mt-4">
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                    {users.map((user) => (
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
                                            onClick={() => onInitiateUserDelete(user)}
                                            disabled={userIDIsDeleting === user.userID}
                                        >
                                            <Trash2/>
                                        </Button>
                                    </CardTitle>
                                </CardHeader>
                                <CardContent>
                                    <UserMovies
                                        pooledMovies={user.currentPool}
                                        stashedMovies={user.stash}
                                        userID={user.userID}
                                        onMovieDelete={(movieID: string) => onMovieDelete(user.userID, movieID)}
                                        onMovieAdd={onMovieAdd}
                                        onMovieMove={(movieID: string) => onMovieMove(user.userID, movieID)}
                                    />
                                </CardContent>
                            </Card>
                        </AnimatedListItem>
                    ))}
                </div>
            </div>

            <ConfirmDialog
                isOpen={!!userToDelete}
                onClose={() => onUserSetDeleting(null)}
                onConfirm={onUserDelete}
                title="Delete User"
                description={`Are you sure you want to delete ${userToDelete?.name}? This action cannot be undone.`}
                confirmText="Delete"
                cancelText="Cancel"
            />
        </div>
    );
};

export default UserManagement;

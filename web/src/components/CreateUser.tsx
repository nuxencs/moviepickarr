import React, {useState} from "react";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import {Plus} from "lucide-react";
import {toast} from "@/components/ui/toast";
import {APIClient} from "@/api/APIClient";
import {User as UserData} from "@/types/Response";

interface AddUserProps {
    onUserCreated: (user: UserData) => void;
}

const CreateUser: React.FC<AddUserProps> = ({onUserCreated}) => {
    const [username, setUsername] = useState("");
    const [isLoading, setIsLoading] = useState(false);

    const createUser = async (e: React.FormEvent) => {
        e.preventDefault();

        if (!username.trim()) {
            toast.error("Username is required");
            return;
        }

        setIsLoading(true);
        const newUser = await APIClient.users.create(username);
        if (newUser) {
            setUsername("")
            onUserCreated(newUser);
            toast.success(`User ${username} added successfully`);
        } else {
            toast.error("Failed to create user");
        }
        setIsLoading(false);
    };

    return (
        <div className="flex gap-2">
            <Input
                type="text"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="Enter username"
                onKeyDown={(e: React.KeyboardEvent) => e.key === 'Enter' && createUser(e)}
                disabled={isLoading}
                required
            />
            <Button
                type="submit"
                onClick={createUser}
                disabled={isLoading || username.length === 0}
            >
                <Plus/>
                {isLoading ? 'Adding...' : 'Add User'}
            </Button>
        </div>
    );
};

export default CreateUser;

import {FormEvent, useState} from 'react';
import {Button} from '@/components/ui/button';
import {Input} from '@/components/ui/input';
import {Plus} from 'lucide-react';
import {toast} from '@/components/ui/toast';
import {APIClient} from "@/api/APIClient";
import {useMutation, useQueryClient} from "@tanstack/react-query";
import {MoviesKeys, UsersKeys} from "@/api/query_keys";

interface AddMovieProps {
    userID: string;
}

export function AddMovie({userID}: AddMovieProps) {
    const [title, setTitle] = useState('');
    const [link, setLink] = useState('');

    const queryClient = useQueryClient();

    const addMutation = useMutation({
        mutationFn: (e: FormEvent) => {
            e.preventDefault()
            return APIClient.users.addMovie(userID, title, link)
        },
        onSuccess: () => {
            toast.success(`Movie ${title} added successfully!`);
            setTitle('')
            setLink('')
            void queryClient.invalidateQueries({queryKey: UsersKeys.list()});
            void queryClient.invalidateQueries({queryKey: MoviesKeys.listpool()});
        },
        onError: () => {
            toast.error(`Error adding movie`);
            setTitle('')
            setLink('')
        }
    })

    return (
        <form onSubmit={addMutation.mutate} className="space-y-2">
            <div className="flex gap-2">
                <Input
                    type="text"
                    value={title}
                    onChange={(e) => setTitle(e.target.value)}
                    placeholder="Enter movie title"
                    disabled={addMutation.isPending}
                />
                <Input
                    type="url"
                    value={link}
                    onChange={(e) => setLink(e.target.value)}
                    placeholder="Enter movie link"
                    disabled={addMutation.isPending}
                />
            </div>
            <Button
                type="submit"
                disabled={addMutation.isPending || (!title.trim() || !link.trim())}
                className="w-full"
            >
                <Plus/>
                Add Movie
            </Button>
        </form>
    );
}

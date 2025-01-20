import React, {useState} from 'react';
import {Button} from '@/components/ui/button';
import {Input} from '@/components/ui/input';
import {Plus} from 'lucide-react';
import {toast} from '@/components/ui/toast';
import {APIClient} from "@/api/APIClient";
import {Movie} from "@/types/Response";

interface AddMovieProps {
    userID: string;
    onMovieAdded: (movie: Movie) => void;
}

const AddMovie: React.FC<AddMovieProps> = ({userID, onMovieAdded}) => {
    const [title, setTitle] = useState('');
    const [link, setLink] = useState('');
    const [isLoading, setIsLoading] = useState(false);

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();

        if (!title.trim() || !link.trim()) {
            toast.error('Please fill in both title and link');
            return;
        }

        setIsLoading(true);
        const addedMovie = await APIClient.users.addMovie(userID, title, link);
        if (addedMovie) {
            onMovieAdded(addedMovie);
            setTitle('')
            setLink('')
            setIsLoading(false);
        }
    };

    return (
        <form onSubmit={handleSubmit} className="space-y-2">
            <div className="flex gap-2">
                <Input
                    type="text"
                    value={title}
                    onChange={(e) => setTitle(e.target.value)}
                    placeholder="Enter movie title"
                    disabled={isLoading}
                />
                <Input
                    type="url"
                    value={link}
                    onChange={(e) => setLink(e.target.value)}
                    placeholder="Enter movie link"
                    disabled={isLoading}
                />
            </div>
            <Button
                type="submit"
                disabled={isLoading || (!title.trim() || !link.trim())}
                className="w-full"
            >
                <Plus/>
                Add Movie
            </Button>
        </form>
    );
};

export default AddMovie;

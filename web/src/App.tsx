import {useEffect, useState} from "react";
import UserManagement from "@/components/UserManagement";
import {Movie, User as UserData} from "@/types/Response";
import {APIClient} from "@/api/APIClient.ts";
import MoviePicker from "@/components/MoviePicker";
import {ThemeProvider} from "@/components/ThemeProvider.tsx"
import {toast, Toaster} from "@/components/ui/toast";
import Header from "@/components/Header";

function App() {
    const [users, setUsers] = useState<UserData[]>([]);
    const [userIsDeleting, setUserIsDeleting] = useState<string | null>(null);
    const [userToDelete, setUserToDelete] = useState<UserData | null>(null);
    const [watchedMovies, setWatchedMovies] = useState<Movie[]>([]);
    const [pooledMovies, setPooledMovies] = useState<Movie[]>([]);
    const [currentMovie, setCurrentMovie] = useState<Movie | null>(null);
    const [isLoading, setIsLoading] = useState<boolean>(false);

    useEffect(() => {
        void fetchUsers()
        void fetchPooledMovies()
        void fetchCurrentMovie()
        void fetchWatchedMovies()
    }, [])

    const fetchUsers = async () => {
        const users = await APIClient.users.getAll();
        setUsers(users as UserData[] || []);
    };

    const fetchCurrentMovie = async () => {
        const currentMovie = await APIClient.movies.getCurrent();
        setCurrentMovie(currentMovie);
    };

    const fetchPooledMovies = async () => {
        const pooledMovies = await APIClient.movies.getPooled();
        setPooledMovies(pooledMovies);
    };

    const fetchWatchedMovies = async () => {
        const watchedMovies = await APIClient.movies.getWatched();
        setWatchedMovies(watchedMovies);
    };

    const handleUserCreated = (newUser: UserData) => {
        setUsers((prevUsers) => [...prevUsers, newUser]);
    };

    const initiateDeleteUser = (user: UserData) => {
        setUserToDelete(user);
    };

    const handleDeleteUser = async () => {
        if (!userToDelete) return;

        setUserIsDeleting(userToDelete.userID);
        await APIClient.users.delete(userToDelete.userID);
        await fetchUsers();
        await fetchPooledMovies();

        setUserIsDeleting(null);
        setUserToDelete(null);
    };

    const handleDeleteMovie = async (userID: string, movieID: string) => {
        await APIClient.users.deleteMovie(userID, movieID);
        await fetchUsers();
        await fetchPooledMovies();
    };

    const handleMoveMovie = async (userID: string, movieID: string) => {
        await APIClient.users.moveMovie(userID, movieID);
        await fetchUsers();
        await fetchPooledMovies();
    };

    const handleMovieAdded = async () => {
        await fetchUsers();
        await fetchPooledMovies();
    };

    const pickRandomMovie = async () => {
        setIsLoading(true);

        const randomMovie = await APIClient.movies.getRandom();
        console.log(randomMovie)
        if (randomMovie) {
            await fetchUsers();
            await fetchPooledMovies();
            await fetchCurrentMovie();
            toast.success('Movie picked successfully');
            setIsLoading(false);
        } else {
            toast.error('Failed to pick a random movie');
        }
    };

    const markAsWatched = async () => {
        if (!currentMovie) return;

        const watchedMovie = await APIClient.movies.markWatched();
        if (watchedMovie) {
            setCurrentMovie(null);
            await fetchPooledMovies();
            await fetchWatchedMovies();
            toast.success('Movie marked as watched');
        } else {
            toast.error('Failed to mark movie as watched');
        }
    };

    return (
        <ThemeProvider defaultTheme="dark" storageKey="vite-ui-theme">
            <div className="App">
                <Header/>
                <UserManagement
                    users={users}
                    userIDIsDeleting={userIsDeleting}
                    userToDelete={userToDelete}
                    onUserCreate={handleUserCreated}
                    onInitiateUserDelete={initiateDeleteUser}
                    onUserSetDeleting={setUserToDelete}
                    onUserDelete={handleDeleteUser}
                    onMovieAdd={handleMovieAdded}
                    onMovieDelete={handleDeleteMovie}
                    onMovieMove={handleMoveMovie}
                />
                <MoviePicker
                    watchedMovies={watchedMovies}
                    pooledMovies={pooledMovies}
                    currentMovie={currentMovie}
                    onPickedMovie={pickRandomMovie}
                    onMarkAsWatched={markAsWatched}
                    isLoading={isLoading}
                />
                <Toaster/>
            </div>
        </ThemeProvider>
    )
}

export default App

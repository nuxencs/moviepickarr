import { APIClient } from "@/api/APIClient";

import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { toast } from '@/components/ui/toast';

import { TMDBMovie } from "@/types/Response";

import { useMutation } from "@tanstack/react-query";
import { AnimatePresence, motion } from "framer-motion";
import { PlusIcon, Search } from 'lucide-react';
import { FormEvent, useEffect, useState } from 'react';
import { createPortal } from 'react-dom';

interface SearchMovieProps {
  userID: string;
}

const TMDB_IMAGE_BASE = 'https://image.tmdb.org/t/p/w500';

export function SearchMovie({ userID }: SearchMovieProps) {
  const [searchQuery, setSearchQuery] = useState('');
  const [searchResults, setSearchResults] = useState<TMDBMovie[]>([]);
  const [isSearching, setIsSearching] = useState(false);
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [isFetchingExternalId, setIsFetchingExternalId] = useState(false);

  const closeModal = () => {
    setIsModalOpen(false);
    setSearchQuery('');
    setSearchResults([]);
  };

  // Handle ESC key to close modal and disable body scroll
  useEffect(() => {
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        closeModal();
      }
    };

    if (isModalOpen) {
      // Disable body scroll
      document.body.style.overflow = 'hidden';
      document.addEventListener('keydown', handleEscape);

      return () => {
        // Re-enable body scroll
        document.body.style.overflow = '';
        document.removeEventListener('keydown', handleEscape);
      };
    }
  }, [isModalOpen]);

  const addMutation = useMutation({
    mutationFn: ({ title, link }: { title: string; link: string }) => {
      return APIClient.users.addMovie(userID, title, link);
    },
    onSuccess: (_, variables) => {
      toast.success(`${variables.title} added successfully!`);
      closeModal();
    },
    onError: (_, variables) => {
      toast.error(`Error adding ${variables.title}`);
    }
  });

  const handleSearch = async (e: FormEvent) => {
    e.preventDefault();
    if (!searchQuery.trim()) return;

    setIsSearching(true);
    try {
      const results = await APIClient.tmdb.search(searchQuery);
      setSearchResults(results);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to search movies');
      setSearchResults([]);
    } finally {
      setIsSearching(false);
    }
  };

  const handleAddMovie = async (movie: TMDBMovie) => {
    if (isFetchingExternalId || addMutation.isPending) return;

    setIsFetchingExternalId(true);
    try {
      const { link } = await APIClient.tmdb.getExternalIds(movie.id);
      addMutation.mutate({
        title: movie.title,
        link: link
      });
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to fetch movie details');
    } finally {
      setIsFetchingExternalId(false);
    }
  };

  return (
    <>
      {/* Search button trigger */}
      <Button onClick={() => setIsModalOpen(true)} className="w-full">
        <Search className="h-4 w-4"/>
        Search Movie
      </Button>

      {/* Modal */}
      {isModalOpen && createPortal(
        <AnimatePresence>
          <div className="fixed inset-0 z-50 flex items-center justify-center px-4" onClick={closeModal}>
            {/* Backdrop with blur */}
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              className="fixed inset-0 bg-black/30 backdrop-blur-md -z-10"
            />

            {/* Modal Content */}
            <motion.div
              initial={{ opacity: 0 }}
              animate={{
                opacity: 1,
                width: 'auto',
              }}
              exit={{ opacity: 0 }}
              transition={{
                opacity: { duration: 0.2 },
                width: { type: 'spring', stiffness: 300, damping: 30, duration: 1 }
              }}
              className="relative max-h-[80vh] flex flex-col"
              onClick={(e) => e.stopPropagation()}
            >
              {/* Search form - fixed at top */}
              <div className="p-6 pb-4">
                <form onSubmit={handleSearch}>
                  <div className="relative bg-accent/80 rounded-md">
                    <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground"/>
                    <Input
                      type="text"
                      value={searchQuery}
                      onChange={(e) => setSearchQuery(e.target.value)}
                      placeholder="Search for a movie..."
                      disabled={isSearching}
                      className="px-10"
                      autoFocus
                    />
                  </div>
                </form>
              </div>

              {/* Search results - scrollable */}
              {searchResults.length > 0 && (
                <div className="flex-1 overflow-y-auto px-6 pb-6">
                  <div className="bg-card/80 rounded-lg border p-4">
                    <AnimatePresence>
                      <motion.div
                        initial={{ opacity: 0, height: 0 }}
                        animate={{ opacity: 1, height: 'auto' }}
                        exit={{ opacity: 0, height: 0 }}
                        transition={{ type: 'spring', stiffness: 300, damping: 30 }}
                      >
                        <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-4">
                          {searchResults.map((movie) => (
                            <motion.div
                              key={movie.id}
                              whileHover={{ scale: 1.05 }}
                              initial={{ opacity: 0 }}
                              animate={{ opacity: 1 }}
                              className="relative group"
                            >
                              <div className="relative aspect-[2/3] overflow-hidden rounded-lg bg-muted">
                                {movie.poster_path ? (
                                  <img
                                    src={`${TMDB_IMAGE_BASE}${movie.poster_path}`}
                                    alt={movie.title}
                                    className="w-full h-full object-cover"
                                  />
                                ) : (
                                  <div
                                    className="w-full h-full flex items-center justify-center text-muted-foreground text-xs">
                                    No Poster
                                  </div>
                                )}

                                <motion.div
                                  initial={{ opacity: 0 }}
                                  whileHover={{ opacity: 1 }}
                                  className="absolute inset-0 bg-black/80 flex flex-col justify-between p-3"
                                >
                                  {/* Overview */}
                                  <div className="overflow-y-auto flex-1">
                                    <p className="text-xs text-white line-clamp-6">
                                      {movie.overview || 'No overview available'}
                                    </p>
                                  </div>

                                  {/* Add button at bottom */}
                                  <Button
                                    onClick={() => handleAddMovie(movie)}
                                    disabled={addMutation.isPending || isFetchingExternalId}
                                    size="sm"
                                    className="w-full gap-2 mt-2"
                                  >
                                    <PlusIcon className="h-4 w-4"/>
                                    {isFetchingExternalId ? 'Loading...' : 'Add Movie'}
                                  </Button>
                                </motion.div>
                              </div>

                              <div className="mt-2">
                                <p className="text-xs font-medium line-clamp-2">{movie.title}</p>
                                {movie.release_date && (
                                  <p className="text-xs text-muted-foreground">
                                    {new Date(movie.release_date).getFullYear()}
                                  </p>
                                )}
                              </div>
                            </motion.div>
                          ))}
                        </div>
                      </motion.div>
                    </AnimatePresence>
                  </div>
                </div>
              )}
            </motion.div>
          </div>
        </AnimatePresence>,
        document.body
      )}
    </>
  );
}
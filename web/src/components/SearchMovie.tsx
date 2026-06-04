import { useMutation } from "@tanstack/react-query";
import { AnimatePresence, motion } from "framer-motion";
import { Loader2, PlusIcon, Search, X } from 'lucide-react';
import { FormEvent, useEffect, useState } from 'react';
import { createPortal } from 'react-dom';

import { APIClient } from "@/api/APIClient";

import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { toast } from '@/components/ui/toast';

import { TMDBMovie } from "@/types/Response";

interface SearchMovieProps {
  userID: number;
}

const TMDB_IMAGE_BASE = 'https://image.tmdb.org/t/p/w500';

export function SearchMovie({ userID }: SearchMovieProps) {
  const [searchQuery, setSearchQuery] = useState('');
  const [searchResults, setSearchResults] = useState<TMDBMovie[]>([]);
  const [hasSearched, setHasSearched] = useState(false);
  const [isSearching, setIsSearching] = useState(false);
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [pendingMovieId, setPendingMovieId] = useState<number | null>(null);
  const [expandedMovieId, setExpandedMovieId] = useState<number | null>(null);
  const [isMobileViewport, setIsMobileViewport] = useState(false);

  const closeModal = () => {
    setIsModalOpen(false);
    setSearchQuery('');
    setSearchResults([]);
    setHasSearched(false);
    setPendingMovieId(null);
    setExpandedMovieId(null);
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

  useEffect(() => {
    const updateViewport = () => {
      const isMobile = window.innerWidth < 640;
      setIsMobileViewport(isMobile);
      if (!isMobile) {
        setExpandedMovieId(null);
      }
    };
    updateViewport();
    window.addEventListener('resize', updateViewport);
    return () => window.removeEventListener('resize', updateViewport);
  }, []);

  const addMutation = useMutation({
    mutationFn: ({ title, link }: { title: string; link: string }) => {
      return APIClient.users.addMovie(userID, title, link);
    },
    onSuccess: (_, variables) => {
      toast.success(`${variables.title} added successfully!`);
      closeModal();
    }
  });

  const handleSearch = async (e: FormEvent) => {
    e.preventDefault();
    if (!searchQuery.trim() || isSearching) return;

    setIsSearching(true);
    setHasSearched(true);
    try {
      const results = await APIClient.tmdb.search(searchQuery.trim());
      setSearchResults(results);
      setExpandedMovieId(null);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to search movies');
      setSearchResults([]);
    } finally {
      setIsSearching(false);
    }
  };

  const handleAddMovie = async (movie: TMDBMovie) => {
    if (pendingMovieId !== null || addMutation.isPending) return;

    setPendingMovieId(movie.id);
    try {
      const { link } = await APIClient.tmdb.getExternalIds(movie.id);
      await addMutation.mutateAsync({
        title: movie.title,
        link: link
      });
    } catch (error) {
      toast.error(error instanceof Error ? error.message : `Failed to add ${movie.title}`);
    } finally {
      setPendingMovieId(null);
    }
  };

  const hasResults = searchResults.length > 0;
  const shouldShowBody = hasSearched || isSearching;
  const expandedTop = isMobileViewport ? 0 : '6dvh';

  return (
    <>
      <Button onClick={() => setIsModalOpen(true)} className="w-full">
        <Search className="h-4 w-4"/>
        Search Movie
      </Button>

      {createPortal(
        <AnimatePresence>
          {isModalOpen && (
            <div className="fixed inset-0 z-50" onClick={closeModal}>
              <motion.div
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                className="absolute inset-0 bg-black/45 backdrop-blur-sm"
              />

              <motion.div
                initial={{ opacity: 0, scale: 0.96, top: '50%', y: '-50%' }}
                animate={{
                  opacity: 1,
                  scale: 1,
                  top: shouldShowBody ? expandedTop : '50%',
                  y: shouldShowBody ? 0 : '-50%',
                }}
                exit={{ opacity: 0, scale: 0.96 }}
                transition={{ duration: 0.2, ease: 'easeOut' }}
                className={`absolute left-1/2 z-10 -translate-x-1/2 bg-background ${
                  shouldShowBody
                    ? 'h-[100dvh] w-full sm:h-[min(86vh,54rem)] sm:max-w-6xl sm:overflow-hidden sm:rounded-2xl sm:border sm:shadow-2xl'
                    : 'h-auto w-[calc(100%-1rem)] overflow-hidden rounded-2xl border shadow-2xl sm:w-[min(44rem,calc(100%-2rem))]'
                }`}
                onClick={(e) => e.stopPropagation()}
              >
                <div
                  className={`z-20 bg-background/95 backdrop-blur-sm ${
                    shouldShowBody ? 'sticky top-0 border-b' : ''
                  }`}
                >
                  <div className="flex items-start justify-between gap-4 px-4 pt-4 sm:px-6 sm:pt-6">
                    <div className="min-w-0">
                      <h2 className="text-base font-semibold sm:text-lg">Search and Add Movies</h2>
                      <p className="text-xs text-muted-foreground sm:text-sm">
                        Find a movie from TMDB, then add it to this stash.
                      </p>
                    </div>
                    <Button variant="ghost" size="icon" onClick={closeModal} aria-label="Close search">
                      <X className="size-4"/>
                    </Button>
                  </div>

                  <form onSubmit={handleSearch} className="px-4 pb-4 pt-3 sm:px-6 sm:pb-5">
                    <div className="flex items-center gap-2 sm:gap-3">
                      <div className="relative flex-1">
                        <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"/>
                        <Input
                          type="text"
                          value={searchQuery}
                          onChange={(e) => setSearchQuery(e.target.value)}
                          placeholder="Search title (e.g. Dune, The Matrix)"
                          disabled={isSearching}
                          className="h-10 pl-9 pr-3"
                          autoFocus
                        />
                      </div>

                      <Button
                        type="submit"
                        size="sm"
                        disabled={!searchQuery.trim() || isSearching}
                        className="h-10 min-w-[6.5rem]"
                      >
                        {isSearching ? <Loader2 className="size-4 animate-spin"/> : <Search className="size-4"/>}
                        <span>{isSearching ? 'Searching' : 'Search'}</span>
                      </Button>
                    </div>
                  </form>

                  <div className="px-4 pb-3 text-xs text-muted-foreground sm:px-6">
                    {hasSearched && !isSearching && `${searchResults.length} ${searchResults.length === 1 ? 'result' : 'results'}`}
                    {isSearching && 'Looking up matches...'}
                  </div>
                </div>

                <AnimatePresence initial={false}>
                  {shouldShowBody && (
                    <motion.div
                      initial={{ opacity: 0, y: 36 }}
                      animate={{ opacity: 1, y: 0 }}
                      exit={{ opacity: 0, y: 36 }}
                      transition={{ duration: 0.2, ease: 'easeOut' }}
                      className="h-[calc(100dvh-12rem)] overflow-y-auto px-4 pb-6 sm:h-[calc(min(86vh,54rem)-12rem)] sm:px-6 sm:pb-6"
                    >
                    {isSearching && (
                      <div className="flex h-full min-h-48 flex-col items-center justify-center gap-3 text-sm text-muted-foreground">
                        <Loader2 className="size-5 animate-spin"/>
                        Searching TMDB...
                      </div>
                    )}

                    {!isSearching && !hasResults && (
                      <div className="mt-3 rounded-xl border border-dashed p-6 text-center text-sm text-muted-foreground">
                        No matches found. Try a broader title or original name.
                      </div>
                    )}

                    {!isSearching && hasResults && (
                      <div className="mt-2.5 grid grid-cols-2 gap-1.5 pb-1.5 sm:grid-cols-3 sm:gap-2.5 md:grid-cols-4 lg:grid-cols-5">
                        {searchResults.map((movie) => {
                          const year = movie.release_date ? new Date(movie.release_date).getFullYear() : undefined;
                          const isAddingThisMovie = pendingMovieId === movie.id;
                          const isExpanded = expandedMovieId === movie.id;
                          return (
                            <motion.div
                              key={movie.id}
                              initial={{ opacity: 0, y: 8 }}
                              animate={{ opacity: 1, y: 0 }}
                              transition={{ duration: 0.18 }}
                              className="group rounded-lg bg-card p-1.5 shadow-xs transition-shadow hover:shadow-md sm:p-2"
                            >
                              {/*
                                Mobile: tap toggles details.
                                Desktop: details show on hover only.
                              */}
                              <div
                                role={isMobileViewport ? 'button' : undefined}
                                tabIndex={isMobileViewport ? 0 : -1}
                                className="relative block aspect-[2/3] w-full overflow-hidden rounded-md bg-muted text-left"
                                onClick={() => {
                                  if (!isMobileViewport) return;
                                  setExpandedMovieId(isExpanded ? null : movie.id);
                                }}
                                onKeyDown={(e) => {
                                  if (!isMobileViewport) return;
                                  if (e.key === 'Enter' || e.key === ' ') {
                                    e.preventDefault();
                                    setExpandedMovieId(isExpanded ? null : movie.id);
                                  }
                                }}
                                aria-expanded={isMobileViewport ? isExpanded : undefined}
                                aria-label={isMobileViewport ? `Toggle details for ${movie.title}` : undefined}
                              >
                                {movie.poster_path ? (
                                  <img
                                    src={`${TMDB_IMAGE_BASE}${movie.poster_path}`}
                                    alt={movie.title}
                                    className="h-full w-full object-cover transition-transform duration-200 group-hover:scale-[1.03]"
                                  />
                                ) : (
                                  <div className="flex h-full w-full items-center justify-center px-2 text-center text-xs text-muted-foreground">
                                    No poster
                                  </div>
                                )}

                                <div
                                  className={`absolute inset-0 flex flex-col justify-between bg-black/80 p-2.5 text-white transition-opacity duration-150 sm:p-3 ${
                                    isExpanded ? 'pointer-events-auto opacity-100' : 'pointer-events-none opacity-0'
                                  } sm:group-hover:pointer-events-auto sm:group-hover:opacity-100`}
                                >
                                  <div className="space-y-1">
                                    <p className="line-clamp-2 text-sm font-semibold leading-tight">{movie.title}</p>
                                    <p className="text-[11px] text-white/80 sm:text-xs">
                                      {year && !Number.isNaN(year) ? year : 'Release year unknown'}
                                    </p>
                                  </div>

                                  <p className="line-clamp-5 text-[11px] leading-relaxed sm:text-xs">
                                    {movie.overview || 'No overview available'}
                                  </p>

                                  <Button
                                    onClick={(e) => {
                                      e.stopPropagation();
                                      handleAddMovie(movie);
                                    }}
                                    disabled={pendingMovieId !== null}
                                    size="sm"
                                    className="w-full gap-2"
                                  >
                                    {isAddingThisMovie ? <Loader2 className="size-4 animate-spin"/> : <PlusIcon className="size-4"/>}
                                    {isAddingThisMovie ? 'Adding...' : 'Add Movie'}
                                  </Button>
                                </div>
                              </div>
                            </motion.div>
                          );
                        })}
                      </div>
                    )}
                    </motion.div>
                  )}
                </AnimatePresence>
              </motion.div>
            </div>
          )}
        </AnimatePresence>,
        document.body
      )}
    </>
  );
}

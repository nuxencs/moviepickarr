import { CalendarClockIcon, FilmIcon, LinkIcon, Loader2Icon, XIcon } from "lucide-react";
import { useEffect, useId, useMemo, useState } from "react";

import { Modal } from "@/components/moviepickarr/Modal";
import { isMovieLink } from "@/components/moviepickarr/movieLink";

interface EditMovieDialogSubmit {
  title: string;
  link: string;
  watchedAt?: string;
}

interface EditMovieDialogProps {
  isOpen: boolean;
  onClose: () => void;
  initialTitle: string;
  initialLink: string;
  initialWatchedAt?: string;
  allowWatchedAtEdit?: boolean;
  isSaving?: boolean;
  onSubmit: (payload: EditMovieDialogSubmit) => void;
}

function toLocalDateTimeInputValue(value?: string): string {
  if (!value) {
    return "";
  }

  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return "";
  }

  const year = parsed.getFullYear();
  const month = `${parsed.getMonth() + 1}`.padStart(2, "0");
  const day = `${parsed.getDate()}`.padStart(2, "0");
  const hours = `${parsed.getHours()}`.padStart(2, "0");
  const minutes = `${parsed.getMinutes()}`.padStart(2, "0");

  return `${year}-${month}-${day}T${hours}:${minutes}`;
}

function toISODateTime(value: string): string | undefined {
  if (!value.trim()) {
    return undefined;
  }

  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return undefined;
  }

  return parsed.toISOString();
}

export function EditMovieDialog({
  isOpen,
  onClose,
  initialTitle,
  initialLink,
  initialWatchedAt,
  allowWatchedAtEdit = false,
  isSaving = false,
  onSubmit,
}: EditMovieDialogProps) {
  const [title, setTitle] = useState(initialTitle);
  const [link, setLink] = useState(initialLink);
  const [linkTouched, setLinkTouched] = useState(false);
  const [watchedAtLocal, setWatchedAtLocal] = useState(toLocalDateTimeInputValue(initialWatchedAt));
  const linkErrorID = useId();

  useEffect(() => {
    if (!isOpen) {
      return;
    }

    setTitle(initialTitle);
    setLink(initialLink);
    setLinkTouched(false);
    setWatchedAtLocal(toLocalDateTimeInputValue(initialWatchedAt));
  }, [initialLink, initialTitle, initialWatchedAt, isOpen]);

  const watchedAtISO = useMemo(() => toISODateTime(watchedAtLocal), [watchedAtLocal]);
  const titleValue = title.trim();
  const linkValue = link.trim();
  const isValidLink = isMovieLink(linkValue);
  let linkError: string | undefined;
  if (linkTouched && !isValidLink) {
    linkError = linkValue ? "Use an IMDb or TMDB movie URL." : "Movie link is required.";
  }
  const isInvalidWatchedAt = allowWatchedAtEdit && watchedAtLocal.trim().length > 0 && !watchedAtISO;
  const isSubmitDisabled = isSaving || !titleValue || !isValidLink || isInvalidWatchedAt;

  if (!isOpen) {
    return null;
  }

  return (
    <Modal label="Edit movie" onClose={onClose} className="modal--form" dismissible={!isSaving}>
      {(close) => (
        <>
          <div className="modal__head">
            <div className="top">
              <div>
                <h3>Edit movie</h3>
                {/* Two readings, not one with a clause bolted on: without the
                    watched date the list is a pair and takes "and", not a
                    trailing comma. The movie modal opens this dialog on films
                    that have no watched date (#237), so that branch is drawn
                    now. */}
                <p>{allowWatchedAtEdit ? "Update the title, link, and watched date." : "Update the title and link."}</p>
              </div>
              <button type="button" className="iconbtn" onClick={close} aria-label="Close" disabled={isSaving}>
                <XIcon />
              </button>
            </div>
          </div>

          <div className="modal__body">
            <label className="field">
              <FilmIcon />
              <input
                name="movie-title"
                aria-label="Movie title"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder="Movie title"
                disabled={isSaving}
              />
            </label>
            <div className="fieldgroup">
              <label className="field" data-invalid={linkError ? true : undefined}>
                <LinkIcon />
                <input
                  type="url"
                  name="movie-link"
                  aria-label="Movie link"
                  aria-required="true"
                  aria-invalid={linkError ? true : undefined}
                  aria-describedby={linkError ? linkErrorID : undefined}
                  value={link}
                  onChange={(e) => setLink(e.target.value)}
                  onBlur={() => setLinkTouched(true)}
                  placeholder="Movie link"
                  inputMode="url"
                  autoCapitalize="none"
                  autoCorrect="off"
                  spellCheck={false}
                  required
                  disabled={isSaving}
                />
              </label>
              {linkError && (
                <p id={linkErrorID} className="field-error" role="alert">
                  {linkError}
                </p>
              )}
            </div>
            {allowWatchedAtEdit && (
              <label className="field">
                <CalendarClockIcon />
                <input
                  type="datetime-local"
                  name="watched-at"
                  aria-label="Watched date and time"
                  value={watchedAtLocal}
                  onChange={(e) => setWatchedAtLocal(e.target.value)}
                  disabled={isSaving}
                />
              </label>
            )}
          </div>

          <div className="modal__foot">
            <button type="button" className="btn btn--ghost" onClick={close} disabled={isSaving}>
              Cancel
            </button>
            <button
              type="button"
              className="btn btn--accent"
              disabled={isSubmitDisabled}
              onClick={() => onSubmit({ title: titleValue, link: linkValue, watchedAt: watchedAtISO })}
            >
              {isSaving && <Loader2Icon className="animate-spin mg-spin" />}
              {isSaving ? "Saving…" : "Save changes"}
            </button>
          </div>
        </>
      )}
    </Modal>
  );
}

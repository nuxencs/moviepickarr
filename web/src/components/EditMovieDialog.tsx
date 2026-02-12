import { useEffect, useMemo, useState } from "react";

import { AlertDialog, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

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
  const [watchedAtLocal, setWatchedAtLocal] = useState(toLocalDateTimeInputValue(initialWatchedAt));

  useEffect(() => {
    if (!isOpen) {
      return;
    }

    setTitle(initialTitle);
    setLink(initialLink);
    setWatchedAtLocal(toLocalDateTimeInputValue(initialWatchedAt));
  }, [initialLink, initialTitle, initialWatchedAt, isOpen]);

  const watchedAtISO = useMemo(() => toISODateTime(watchedAtLocal), [watchedAtLocal]);
  const titleValue = title.trim();
  const linkValue = link.trim();
  const isInvalidWatchedAt = allowWatchedAtEdit && watchedAtLocal.trim().length > 0 && !watchedAtISO;
  const isSubmitDisabled = isSaving || !titleValue || !linkValue || isInvalidWatchedAt;

  return (
    <AlertDialog open={isOpen} onOpenChange={onClose}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Edit Movie</AlertDialogTitle>
          <AlertDialogDescription>Update movie details.</AlertDialogDescription>
        </AlertDialogHeader>

        <div className="space-y-3">
          <Input
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="Movie title"
            disabled={isSaving}
          />
          <Input
            type="url"
            value={link}
            onChange={(e) => setLink(e.target.value)}
            placeholder="Movie link"
            disabled={isSaving}
          />

          {allowWatchedAtEdit ? (
            <Input
              type="datetime-local"
              value={watchedAtLocal}
              onChange={(e) => setWatchedAtLocal(e.target.value)}
              disabled={isSaving}
            />
          ) : null}
        </div>

        <AlertDialogFooter>
          <AlertDialogCancel disabled={isSaving}>Cancel</AlertDialogCancel>
          <Button
            disabled={isSubmitDisabled}
            onClick={() => onSubmit({ title: titleValue, link: linkValue, watchedAt: watchedAtISO })}
          >
            Save
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

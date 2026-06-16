import { toast as sonnerToast } from 'sonner';

/** Imperative toast API themed by the `Toaster` mounted in the app shell. */
export const toast = {
    success: (message: string) => sonnerToast.success(message),
    error: (message: string) => sonnerToast.error(message),
    info: (message: string) => sonnerToast.info(message),
};

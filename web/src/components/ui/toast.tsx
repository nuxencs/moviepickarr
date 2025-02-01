import { toast as sonnerToast, Toaster as SonnerToaster } from 'sonner';

export const Toaster = () => {
    return <SonnerToaster position="bottom-right" />;
};

export const toast = {
    success: (message: string) => sonnerToast.success(message),
    error: (message: string) => sonnerToast.error(message),
    info: (message: string) => sonnerToast.info(message),
};

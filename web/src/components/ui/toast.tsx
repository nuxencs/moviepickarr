import { toast as sonnerToast, Toaster as SonnerToaster } from 'sonner';

/** Sonner toaster themed to the Movie Gang surfaces (dark + light via tokens). */
export const Toaster = () => {
    return (
        <SonnerToaster
            position="bottom-right"
            toastOptions={{
                classNames: {
                    toast: 'mg-toast',
                    title: 'mg-toast__title',
                    description: 'mg-toast__desc',
                    icon: 'mg-toast__icon',
                },
            }}
        />
    );
};

export const toast = {
    success: (message: string) => sonnerToast.success(message),
    error: (message: string) => sonnerToast.error(message),
    info: (message: string) => sonnerToast.info(message),
};

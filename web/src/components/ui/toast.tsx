import { Toaster as SonnerToaster } from 'sonner';

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

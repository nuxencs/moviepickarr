import { XIcon } from "lucide-react";
import React from "react";

import { Modal } from "@/components/movie-gang/Modal";

interface ConfirmDialogProps {
    isOpen: boolean;
    onClose: () => void;
    onConfirm: () => void;
    title: string;
    description: string;
    confirmText?: string;
    cancelText?: string;
}

/**
 * Destructive-confirm dialog on the bespoke Modal. Dismissing (Esc, veil click,
 * Cancel) is the safe choice (it does NOT delete), so outside-click dismiss is
 * intentional; only the explicit danger button confirms.
 */
export const DeletionDialog: React.FC<ConfirmDialogProps> = ({
    isOpen,
    onClose,
    onConfirm,
    title,
    description,
    confirmText = "Continue",
    cancelText = "Cancel",
}) => {
    if (!isOpen) {
        return null;
    }

    return (
        <Modal onClose={onClose} className="modal--form">
            {(close) => (
                <>
                    <div className="modal__head">
                        <div className="top">
                            <div>
                                <h3>{title}</h3>
                                <p>{description}</p>
                            </div>
                            <button type="button" className="iconbtn" onClick={close} aria-label="Close">
                                <XIcon />
                            </button>
                        </div>
                    </div>

                    <div className="modal__foot">
                        <button type="button" className="btn btn--ghost" onClick={close}>
                            {cancelText}
                        </button>
                        <button
                            type="button"
                            className="btn btn--danger"
                            onClick={() => {
                                onConfirm();
                                close();
                            }}
                        >
                            {confirmText}
                        </button>
                    </div>
                </>
            )}
        </Modal>
    );
};

import { XIcon } from "lucide-react";
import React from "react";

import { Modal } from "@/components/moviepickarr/Modal";

interface ConfirmDialogProps {
    isOpen: boolean;
    pending: boolean;
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
 * intentional before submission. A pending delete pins the dialog until the
 * parent reports success or failure.
 */
export const DeletionDialog: React.FC<ConfirmDialogProps> = ({
    isOpen,
    pending,
    onClose,
    onConfirm,
    title,
    description,
    confirmText = "Delete",
    cancelText = "Cancel",
}) => {
    if (!isOpen) {
        return null;
    }

    return (
        <Modal onClose={onClose} className="modal--form" dismissible={!pending}>
            {(close) => (
                <>
                    <div className="modal__head">
                        <div className="top">
                            <div>
                                <h3>{title}</h3>
                                <p>{description}</p>
                            </div>
                            <button type="button" className="iconbtn" onClick={close} aria-label="Close" disabled={pending}>
                                <XIcon />
                            </button>
                        </div>
                    </div>

                    <div className="modal__foot">
                        <button type="button" className="btn btn--ghost" onClick={close} disabled={pending}>
                            {cancelText}
                        </button>
                        <button
                            type="button"
                            className="btn btn--danger"
                            onClick={onConfirm}
                            disabled={pending}
                        >
                            {pending ? "Deleting…" : confirmText}
                        </button>
                    </div>
                </>
            )}
        </Modal>
    );
};

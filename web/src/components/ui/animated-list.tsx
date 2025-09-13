import { AnimatePresence, motion } from 'framer-motion';
import React from 'react';

interface AnimatedListProps {
    children: React.ReactNode;
    id: string;
    className?: string;
}

export const AnimatedListItem: React.FC<AnimatedListProps> = ({ children, id, className }) => {
    return (
        <AnimatePresence mode="popLayout">
            <motion.div
                key={id}
                className={className}
                initial={{ opacity: 0, scale: 0.8 }}
                animate={{ opacity: 1, scale: 1 }}
                exit={{ opacity: 0, scale: 0.8 }}
                transition={{ duration: 0.2 }}
            >
                {children}
            </motion.div>
        </AnimatePresence>
    );
};

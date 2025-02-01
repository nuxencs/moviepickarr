import { AnimatePresence, motion } from 'framer-motion';
import React from 'react';

interface AnimatedListProps {
    children: React.ReactNode;
    id: string;
}

export const AnimatedListItem: React.FC<AnimatedListProps> = ({ children, id }) => {
    return (
        <AnimatePresence mode="popLayout">
            <motion.div
                key={id}
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

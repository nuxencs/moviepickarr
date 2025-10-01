import { AnimatePresence, motion } from 'framer-motion';
import React from 'react';

interface AnimatedListProps {
    children: React.ReactNode;
    className?: string;
}

interface AnimatedListItemProps {
    children: React.ReactNode;
    id: string;
    className?: string;
}

const itemVariants = {
    hidden: { opacity: 0 },
    visible: { opacity: 1 },
    exit: { opacity: 0 }
};

export const AnimatedList: React.FC<AnimatedListProps> = ({ children, className }) => {
    const childCount = React.Children.count(children);
    const staggerDelay = Math.max(0.01, 0.3 / childCount);

    const listVariants = {
        hidden: { opacity: 0 },
        visible: {
            opacity: 1,
            transition: {
                staggerChildren: staggerDelay
            }
        }
    };

    return (
        <motion.div
            className={className}
            variants={listVariants}
            initial="hidden"
            animate="visible"
        >
            {children}
        </motion.div>
    );
};

export const AnimatedListItem: React.FC<AnimatedListItemProps> = ({ children, id, className }) => {
    return (
        <AnimatePresence mode="popLayout">
            <motion.div
                key={id}
                className={className}
                variants={itemVariants}
                transition={{ duration: 0.3, ease: 'easeInOut' }}
            >
                {children}
            </motion.div>
        </AnimatePresence>
    );
};

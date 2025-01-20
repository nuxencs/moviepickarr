import {CardTitle} from "@/components/ui/card.tsx";
import {ModeToggle} from "@/components/ui/mode-toggle.tsx";
import React from "react";

const Header: React.FC = () => {
    return (
        <div className="mt-4">
            <CardTitle className="p-4 flex items-center justify-between text-5xl">
                Movie Gang
                <ModeToggle/>
            </CardTitle>
        </div>
    )
}

export default Header;

import {CardTitle} from "@/components/ui/card";
import {ModeToggle} from "@/components/ui/mode-toggle";

export function Header() {
    return (
        <div className="mt-4">
            <CardTitle className="p-4 flex items-center justify-between text-5xl">
                Movie Gang
                <ModeToggle/>
            </CardTitle>
        </div>
    )
}

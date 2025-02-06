import { ModeToggle } from "@/components/ui/mode-toggle";

export function Header() {
    return (
        <div className="mt-4">
            <div className="p-4 flex items-center justify-between text-5xl font-semibold leading-none tracking-tight">
                Movie Gang
                <ModeToggle />
            </div>
        </div>
    )
}

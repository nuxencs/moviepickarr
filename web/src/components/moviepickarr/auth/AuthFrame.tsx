import { Marquee } from "@/components/moviepickarr/auth/Marquee";

import "@/components/moviepickarr/auth/auth.css";

// The split-marquee shell shared by every login and claim state: the cinematic
// panel on the left, the content column on the right. Each screen (form or
// terminal message) drops into the column, so they all sit in one frame.
export function AuthFrame({ children }: { children: React.ReactNode }) {
  return (
    <div className="auth">
      <Marquee />
      <section className="auth__panel">
        <div className="auth__panelinner">{children}</div>
      </section>
    </div>
  );
}

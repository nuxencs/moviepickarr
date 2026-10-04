import { Marquee } from "@/components/moviepickarr/auth/Marquee";

import "@/components/moviepickarr/auth/auth.css";

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

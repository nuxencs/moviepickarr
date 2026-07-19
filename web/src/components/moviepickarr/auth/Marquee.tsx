import { FilmIcon } from "lucide-react";

// Faux poster wall — a fixed set of gradient tiles standing in for artwork, so
// the panel reads as "movie night" without shipping or fetching real posters.
// Each pair is two hues fed into the tile's diagonal gradient.
const TILES = [
  "197 58",
  "84 62",
  "264 30",
  "22 40",
  "150 55",
  "310 45",
  "84 40",
  "264 55",
  "22 62",
];

/** The cinematic left panel shared by the login and claim screens. Decorative
 *  only (aria-hidden): the wordmark and tagline are ambiance, not content the
 *  form column depends on. */
export function Marquee() {
  return (
    <aside className="auth__stage" aria-hidden>
      <div className="auth__wall">
        {TILES.map((hues, i) => {
          const [a, b] = hues.split(" ");
          return (
            <span
              key={i}
              className="auth__tile"
              style={{
                background: `linear-gradient(150deg, oklch(0.4 0.09 ${a}), oklch(0.2 0.05 ${b}))`,
              }}
            />
          );
        })}
      </div>
      <div className="auth__veil" />
      <div className="auth__stagecopy">
        <div className="auth__brand">
          <span className="mark">
            <FilmIcon />
          </span>
          moviepickarr
        </div>
        <p className="auth__tag">Pick tonight&rsquo;s movie together.</p>
      </div>
    </aside>
  );
}

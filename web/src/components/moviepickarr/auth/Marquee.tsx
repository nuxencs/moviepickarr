// Faux poster wall — gradient tiles standing in for artwork, arranged as four
// columns at staggered depths (see auth.css) so the panel reads as a receding
// wall of "movie night" posters without shipping or fetching real artwork. Real
// posters swap in later. Each entry is two hues fed into a tile's diagonal
// gradient; the columns shingle unevenly for depth rather than form a flat grid.
const COLUMNS = [
  ["197 58", "84 40", "150 55", "22 40"],
  ["84 62", "264 55", "310 45", "197 30"],
  ["264 30", "22 62", "84 55", "150 40"],
  ["22 40", "150 30", "264 45", "310 58"],
];

/** The cinematic left panel shared by the login and claim screens. Decorative
 *  only (aria-hidden): a tilted, layered-depth wall of poster stand-ins that the
 *  form column does not depend on. */
export function Marquee() {
  return (
    <aside className="auth__stage" aria-hidden>
      <div className="auth__wall">
        {COLUMNS.map((tiles, ci) => (
          <div key={ci} className="auth__col">
            {tiles.map((hues, ti) => {
              const [a, b] = hues.split(" ");
              return (
                <span
                  key={ti}
                  className="auth__tile"
                  style={{
                    background: `linear-gradient(150deg, oklch(0.4 0.09 ${a}), oklch(0.2 0.05 ${b}))`,
                  }}
                />
              );
            })}
          </div>
        ))}
      </div>
      <div className="auth__veil" />
    </aside>
  );
}

const MON_SHORT = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

export function fmtRange(start: Date | null, end: Date | null): string {
  if (!start) return "Select a start date";
  const s = `${MON_SHORT[start.getMonth()]} ${start.getDate()}`;
  if (!end) return `${s} — …`;
  return `${s} — ${MON_SHORT[end.getMonth()]} ${end.getDate()}, ${end.getFullYear()}`;
}

/** Compact "May 5 – May 19" label for the stats eyebrow. */
export function shortRange(start?: Date | null, end?: Date | null): string | null {
  if (!start || !end) return null;
  return `${MON_SHORT[start.getMonth()]} ${start.getDate()} – ${MON_SHORT[end.getMonth()]} ${end.getDate()}`;
}

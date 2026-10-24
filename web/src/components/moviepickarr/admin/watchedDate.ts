/** A stored instant as a `datetime-local` value in the viewer's zone, or "". */
export function toLocalDateTimeInputValue(value?: string): string {
  if (!value) {
    return "";
  }

  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return "";
  }

  const year = parsed.getFullYear();
  const month = `${parsed.getMonth() + 1}`.padStart(2, "0");
  const day = `${parsed.getDate()}`.padStart(2, "0");
  const hours = `${parsed.getHours()}`.padStart(2, "0");
  const minutes = `${parsed.getMinutes()}`.padStart(2, "0");

  return `${year}-${month}-${day}T${hours}:${minutes}`;
}

/**
 * A `datetime-local` value as ISO, or undefined. The round trip refuses a wall
 * time the zone skips, such as 02:30 on a spring-forward night.
 */
export function toISODateTime(value: string): string | undefined {
  const normalized = value.trim();
  if (!normalized) {
    return undefined;
  }

  const parsed = new Date(normalized);
  if (Number.isNaN(parsed.getTime())) {
    return undefined;
  }

  const iso = parsed.toISOString();
  return toLocalDateTimeInputValue(iso) === normalized ? iso : undefined;
}

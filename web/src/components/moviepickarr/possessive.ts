/**
 * The possessive form of a display name: "Aleks'", "Ada's". Only a literal
 * trailing s drops the s, so "Alex" and "Beatriz" keep 's by design.
 */
export function possessive(name: string): string {
  if (name === "") return "";
  return /s$/i.test(name) ? `${name}'` : `${name}'s`;
}

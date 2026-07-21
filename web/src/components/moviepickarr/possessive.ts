/**
 * The possessive form of a display name. Names ending in s take a bare
 * apostrophe ("Aleks'"), everything else takes 's ("Ada's"). The rule keys on a
 * literal trailing s only, so x/z endings ("Alex", "Beatriz") keep 's by design.
 * One home for the rule so every "<name>'s turn / stash / password" reads the same.
 */
export function possessive(name: string): string {
  if (name === "") return "";
  return /s$/i.test(name) ? `${name}'` : `${name}'s`;
}

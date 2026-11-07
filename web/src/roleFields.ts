function hasASCIIControl(value: string): boolean {
  for (const character of value) {
    const code = character.charCodeAt(0);
    if (code < 0x20 || code === 0x7f) return true;
  }
  return false;
}

export function trimRoleName(name: string): string {
  return name.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, "");
}

export function roleDetailsError(name: string, description: string): string {
  if (!name || Array.from(name).length > 64 || hasASCIIControl(name))
    return "Enter a role name with 1–64 characters and no control characters.";
  if (Array.from(description).length > 500 || description.includes("\0"))
    return "Enter a description with up to 500 characters and no null characters.";
  return "";
}

export function validRoleID(id: string): boolean {
  return !!id && new TextEncoder().encode(id).length <= 256 && !hasASCIIControl(id);
}

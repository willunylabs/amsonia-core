const asciiControl = /[\u0000-\u001f\u007f]/u;

export function trimRoleName(name: string): string {
  return name.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, "");
}

export function roleDetailsError(name: string, description: string): string {
  if (!name || Array.from(name).length > 64 || asciiControl.test(name))
    return "Enter a role name with 1–64 characters and no control characters.";
  if (Array.from(description).length > 500 || description.includes("\0"))
    return "Enter a description with up to 500 characters and no null characters.";
  return "";
}

export function validRoleID(id: string): boolean {
  return !!id && new TextEncoder().encode(id).length <= 256 && !asciiControl.test(id);
}

// Match the API's Unicode code point limits, rather than UTF-16 input lengths.
export function validNewPassword(password: string): boolean {
  const length = Array.from(password).length;
  return length >= 12 && length <= 128;
}

export const PASSWORD_LENGTH_MESSAGE = "Use 12–128 characters for your password.";

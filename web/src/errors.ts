import { APIError } from "./api";
export function friendlyError(error: unknown): string {
  return error instanceof APIError
    ? error.message
    : "Something went wrong. Please try again.";
}

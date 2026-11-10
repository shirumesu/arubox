/** Turns a rejected binding call into the message the Go side returned. */
export function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

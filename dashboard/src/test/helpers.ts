/** Build a JWT-shaped string with the given payload (header/signature are dummies). */
export function makeToken(payload: object): string {
  return `header.${btoa(JSON.stringify(payload))}.signature`
}

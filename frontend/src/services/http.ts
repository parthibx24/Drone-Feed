/** Thin fetch wrapper for the gateway API (same origin, cookie session). */

export const AUTH_EXPIRED_EVENT = "starwatch:auth-expired";

/** Every state-changing request carries this header; the server rejects
 * cookie-authenticated writes without it (CSRF protection). */
const CSRF_HEADER = { "X-Drone-Feed": "1" };

export class ApiError extends Error {
  readonly status: number;
  /** MAVLink result when the drone refused a command (e.g. "DENIED", "FAILED"). */
  readonly result?: string;

  constructor(status: number, message: string, result?: string) {
    super(message);
    this.status = status;
    this.result = result;
  }
}

export async function request<T>(url: string, init: RequestInit & { json?: unknown } = {}): Promise<T> {
  const { json, headers, ...rest } = init;
  const method = (rest.method ?? "GET").toUpperCase();
  const finalHeaders: Record<string, string> = { ...(headers as Record<string, string>) };
  if (method !== "GET" && method !== "HEAD") Object.assign(finalHeaders, CSRF_HEADER);
  if (json !== undefined) finalHeaders["Content-Type"] = "application/json";

  const response = await fetch(url, {
    ...rest,
    method,
    credentials: "same-origin",
    headers: finalHeaders,
    body: json !== undefined ? JSON.stringify(json) : rest.body,
  });

  const body = (await response.json().catch(() => null)) as
    | { error?: string; detail?: string; result?: string }
    | null;
  if (!response.ok) {
    if (response.status === 401 && !url.startsWith("/api/auth/")) {
      window.dispatchEvent(new Event(AUTH_EXPIRED_EVENT));
    }
    throw new ApiError(response.status, body?.error ?? body?.detail ?? `HTTP ${response.status}`, body?.result);
  }
  return body as T;
}

/** Human-readable error text for toasts. */
export function errorText(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 503) return "Drone offline";
    if (error.status === 504) return "No answer from the drone";
    if (error.status === 403) return error.message.includes("operator") ? "Operator role required" : error.message;
    return error.result ? `Drone refused: ${error.result}` : error.message;
  }
  return error instanceof Error ? error.message : String(error);
}

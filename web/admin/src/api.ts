export interface User {
  Id: string;
  Name: string;
  IsAdministrator: boolean;
  IsDisabled: boolean;
  HasPassword: boolean;
  CreatedAt: string;
}

export interface BootstrapResponse {
  Initialized: boolean;
}

export interface UserResponse {
  User: User;
}

export interface SessionResponse extends UserResponse {
  CSRFToken: string;
}

export interface OverviewResponse {
  Server: {
    Id: string;
    Name: string;
    Version: string;
  };
  Database: {
    Status: string;
  };
  Counts: {
    Users: number;
    Libraries: number;
    Items: number;
    ActiveSessions: number;
  };
  Runtime: {
    GoVersion: string;
  };
  Features: {
    LibraryManagement: boolean;
    Playback: boolean;
    Transcoding: boolean;
  };
}

export interface UsersResponse {
  Items: User[];
  TotalRecordCount: number;
}

export interface Library {
  Id: string;
  Name: string;
  CollectionType: string;
  Paths: string[];
  CreatedAt: string;
  LastScanAt: string | null;
}

export interface Job {
  Id: string;
  LibraryId: string;
  Status: string;
  Error: string;
  Scanned: number;
  Added: number;
  Updated: number;
  CreatedAt: string;
  StartedAt: string | null;
  FinishedAt: string | null;
}

export interface LibrariesResponse {
  Items: Library[];
  TotalRecordCount: number;
}

export interface LibraryResponse {
  Library: Library;
  Job?: Job;
  ScanError?: {
    Code: string;
    Message: string;
  };
}

export interface JobsResponse {
  Items: Job[];
  TotalRecordCount: number;
}

export interface JobResponse {
  Job: Job;
}

export interface StorageRootsResponse {
  Items: {
    Path: string;
    Available: boolean;
  }[];
  Configured: boolean;
}

export interface LoginInput {
  Name: string;
  Password: string;
}

export interface BootstrapInput extends LoginInput {
  SetupToken: string;
}

export interface CreateUserInput extends LoginInput {
  IsAdministrator: boolean;
}

export interface LibraryInput {
  Name: string;
  CollectionType: "movies" | "tvshows" | "music" | "mixed";
  Paths: string[];
  Scan: boolean;
}

export interface RequestOptions {
  signal?: AbortSignal;
}

export type ApiFields = Record<string, string | string[]>;

interface ApiErrorOptions {
  status?: number;
  code?: string;
  fields?: ApiFields;
  requestId?: string;
  retryAfterSeconds?: number;
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly fields: ApiFields | undefined;
  readonly requestId: string | undefined;
  readonly retryAfterSeconds: number | undefined;
  readonly retryAt: Date | undefined;

  constructor(message: string, options: ApiErrorOptions = {}) {
    super(message);
    this.name = "ApiError";
    this.status = options.status ?? 0;
    this.code = options.code ?? "request_failed";
    this.fields = options.fields;
    this.requestId = options.requestId;
    this.retryAfterSeconds = options.retryAfterSeconds;
    this.retryAt = options.retryAfterSeconds === undefined
      ? undefined
      : new Date(Date.now() + options.retryAfterSeconds * 1000);
  }
}

const API_ROOT = "/admin/v1";
const sessionExpiredListeners = new Set<() => void>();
let csrfToken: string | undefined;
let sessionRevision = 0;
let sessionExpiredReported = false;

export function isAbortError(error: unknown): boolean {
  return isRecord(error) && error.name === "AbortError";
}

export function clearSession(): void {
  csrfToken = undefined;
  sessionRevision += 1;
}

export function onSessionExpired(callback: () => void): () => void {
  sessionExpiredListeners.add(callback);
  return () => {
    sessionExpiredListeners.delete(callback);
  };
}

function expireSession(revision: number): void {
  // A late response from an older session must not sign out a new login.
  if (revision !== sessionRevision) return;
  clearSession();
  if (sessionExpiredReported) return;
  sessionExpiredReported = true;
  for (const callback of sessionExpiredListeners) {
    try {
      callback();
    } catch {
      // A listener must not replace the original API error.
    }
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function nonemptyString(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() ? value : undefined;
}

function readFields(value: unknown): ApiFields | undefined {
  if (!isRecord(value)) return undefined;
  const fields: ApiFields = Object.create(null) as ApiFields;
  for (const [key, field] of Object.entries(value)) {
    if (typeof field === "string") fields[key] = field;
    else if (Array.isArray(field) && field.every((item) => typeof item === "string")) {
      fields[key] = field as string[];
    }
  }
  return Object.keys(fields).length > 0 ? fields : undefined;
}

function readRetryAfter(value: string | null): number | undefined {
  if (!value?.trim()) return undefined;
  const trimmed = value.trim();
  if (/^\d+(\.\d+)?$/.test(trimmed)) {
    const seconds = Number(trimmed);
    return Number.isFinite(seconds) ? Math.ceil(seconds) : undefined;
  }
  const timestamp = Date.parse(trimmed);
  return Number.isFinite(timestamp)
    ? Math.max(0, Math.ceil((timestamp - Date.now()) / 1000))
    : undefined;
}

function retryHint(seconds: number): string {
  if (seconds === 0) return " You can try again now.";
  const amount = seconds >= 3600 ? Math.ceil(seconds / 3600)
    : seconds >= 60 ? Math.ceil(seconds / 60) : seconds;
  const unit = seconds >= 3600 ? "hour" : seconds >= 60 ? "minute" : "second";
  return ` Try again in about ${amount} ${unit}${amount === 1 ? "" : "s"}.`;
}

function fallbackMessage(status: number, isLogin: boolean): string {
  if (status === 401) return isLogin
    ? "The username or password was not accepted."
    : "Your administrator session has expired. Sign in again.";
  if (status === 403) return "You do not have permission to perform this action.";
  if (status === 404) return "The requested resource could not be found.";
  if (status === 409) return "This change conflicts with the current server state.";
  if (status === 429) return "Too many requests. Please wait before trying again.";
  if (status >= 500) return "The server could not complete the request. Please try again.";
  return "The request could not be completed.";
}

function responseError(response: Response, payload: unknown, isLogin: boolean): ApiError {
  const envelope = isRecord(payload) ? payload : {};
  const error = isRecord(envelope.Error) ? envelope.Error : {};
  const retryAfterSeconds = readRetryAfter(response.headers.get("Retry-After"));
  const message = nonemptyString(error.Message) ?? fallbackMessage(response.status, isLogin);
  return new ApiError(message + (retryAfterSeconds === undefined ? "" : retryHint(retryAfterSeconds)), {
    status: response.status,
    code: nonemptyString(error.Code) ?? `http_${response.status}`,
    fields: readFields(error.Fields),
    requestId: nonemptyString(envelope.RequestId)
      ?? nonemptyString(response.headers.get("X-Request-Id")),
    retryAfterSeconds,
  });
}

function abortIfNeeded(signal?: AbortSignal): void {
  if (signal?.aborted) throw new DOMException("The request was cancelled.", "AbortError");
}

function invalidResponse(status = 200): ApiError {
  return new ApiError("The server returned an invalid response. Please try again.", {
    status,
    code: "invalid_response",
  });
}

interface InternalRequestOptions extends RequestOptions {
  method?: "GET" | "POST" | "DELETE";
  body?: unknown;
  public?: boolean;
}

async function request<T>(path: string, options: InternalRequestOptions = {}): Promise<T> {
  abortIfNeeded(options.signal);
  const method = options.method ?? "GET";
  const revision = sessionRevision;
  const headers = new Headers({ Accept: "application/json" });
  if (options.body !== undefined) headers.set("Content-Type", "application/json");
  if (method !== "GET" && !options.public && csrfToken) {
    headers.set("X-CSRF-Token", csrfToken);
  }

  let response: Response;
  let text: string;
  try {
    response = await fetch(`${API_ROOT}${path}`, {
      method,
      headers,
      credentials: "same-origin",
      cache: "no-store",
      signal: options.signal,
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
    });
    text = await response.text();
  } catch (error) {
    abortIfNeeded(options.signal);
    if (isAbortError(error)) throw error;
    throw new ApiError("Unable to reach the server. Check your connection and try again.", {
      code: "network_error",
    });
  }
  abortIfNeeded(options.signal);

  let payload: unknown;
  try {
    payload = text ? JSON.parse(text) : undefined;
  } catch {
    // Non-JSON error pages still become useful HTTP errors below.
    if (response.ok) throw invalidResponse(response.status);
  }
  if (!response.ok) {
    if (response.status === 401 && !options.public) expireSession(revision);
    throw responseError(response, payload, path === "/session" && method === "POST");
  }
  if (response.status === 204) return undefined as T;
  if (!isRecord(payload)) throw invalidResponse(response.status);
  return payload as T;
}

function validateSession(session: SessionResponse): void {
  if (!isRecord(session.User) || !nonemptyString(session.CSRFToken)) {
    throw invalidResponse();
  }
}

function sessionChanged(): ApiError {
  return new ApiError("Your session changed. Please try the action again.", {
    code: "session_changed",
  });
}

async function getSession(options: RequestOptions = {}): Promise<SessionResponse> {
  const revision = sessionRevision;
  const session = await request<SessionResponse>("/session", options);
  validateSession(session);
  if (revision !== sessionRevision) throw sessionChanged();
  csrfToken = session.CSRFToken;
  sessionExpiredReported = false;
  return session;
}

async function mutate<T>(
  path: string,
  method: "POST" | "DELETE",
  body: unknown,
  options: RequestOptions,
): Promise<T> {
  const revision = sessionRevision;
  if (!csrfToken) {
    expireSession(revision);
    throw sessionChanged();
  }
  try {
    return await request<T>(path, { ...options, method, body });
  } catch (error) {
    // Cookies are shared across tabs. A different CSRF token may mean a different
    // administrator signed in elsewhere, so never replay an old form as that user.
    const rejectedCSRF = error instanceof ApiError && error.status === 403
      && ["csrf_invalid", "invalid_csrf_token", "csrf_token_invalid"].includes(error.code);
    if (!rejectedCSRF || revision !== sessionRevision) throw error;
    expireSession(revision);
    throw new ApiError("Your administrator session changed. Sign in again before making changes.", {
      status: error.status,
      code: "session_changed",
      requestId: error.requestId,
    });
  }
}

export const adminApi = {
  getBootstrap(options: RequestOptions = {}): Promise<BootstrapResponse> {
    return request("/bootstrap", { ...options, public: true });
  },

  bootstrap(input: BootstrapInput, options: RequestOptions = {}): Promise<UserResponse> {
    return request("/bootstrap", { ...options, method: "POST", body: input, public: true });
  },

  async login(input: LoginInput, options: RequestOptions = {}): Promise<SessionResponse> {
    const session = await request<SessionResponse>("/session", {
      ...options,
      method: "POST",
      body: input,
      public: true,
    });
    validateSession(session);
    clearSession();
    csrfToken = session.CSRFToken;
    sessionExpiredReported = false;
    return session;
  },

  getSession,

  async logout(options: RequestOptions = {}): Promise<void> {
    const revision = sessionRevision;
    await mutate<void>("/session", "DELETE", undefined, options);
    if (revision === sessionRevision) clearSession();
  },

  getOverview(options: RequestOptions = {}): Promise<OverviewResponse> {
    return request("/overview", options);
  },

  getUsers(options: RequestOptions = {}): Promise<UsersResponse> {
    return request("/users", options);
  },

  createUser(input: CreateUserInput, options: RequestOptions = {}): Promise<UserResponse> {
    return mutate("/users", "POST", input, options);
  },

  getLibraries(options: RequestOptions = {}): Promise<LibrariesResponse> {
    return request("/libraries", options);
  },

  createLibrary(input: LibraryInput, options: RequestOptions = {}): Promise<LibraryResponse> {
    return mutate("/libraries", "POST", input, options);
  },

  deleteLibrary(libraryId: string, options: RequestOptions = {}): Promise<void> {
    return mutate(`/libraries/${encodeURIComponent(libraryId)}`, "DELETE", undefined, options);
  },

  scanLibrary(libraryId: string, options: RequestOptions = {}): Promise<JobResponse> {
    return mutate(`/libraries/${encodeURIComponent(libraryId)}/scan`, "POST", undefined, options);
  },

  getJobs(options: RequestOptions = {}): Promise<JobsResponse> {
    return request("/jobs", options);
  },

  cancelJob(jobId: string, options: RequestOptions = {}): Promise<JobResponse> {
    return mutate(`/jobs/${encodeURIComponent(jobId)}/cancel`, "POST", undefined, options);
  },

  getStorageRoots(options: RequestOptions = {}): Promise<StorageRootsResponse> {
    return request("/storage/roots", options);
  },
};

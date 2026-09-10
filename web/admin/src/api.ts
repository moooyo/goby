export interface User {
  Id: string;
  Name: string;
  IsAdministrator: boolean;
  IsDisabled: boolean;
  HasPassword: boolean;
  CreatedAt: string;
}

export interface UserPolicy {
  EnableAllFolders: boolean;
  EnabledFolders: string[];
  EnableMediaPlayback: boolean;
  EnablePlaybackRemuxing: boolean;
  EnableAudioPlaybackTranscoding: boolean;
  EnableVideoPlaybackTranscoding: boolean;
}

export interface ManagedUser extends User {
  Revision: string;
  Policy: UserPolicy;
}

export interface ManagedUserResponse {
  User: ManagedUser;
}

export interface UserMutationResponse extends ManagedUserResponse {
  CurrentSessionRevoked: boolean;
}

export interface UpdateUserInput {
  Revision: string;
  Name: string;
  IsAdministrator: boolean;
  IsDisabled: boolean;
  Policy: UserPolicy;
}

export interface ResetUserPasswordInput {
  Revision: string;
  Password: string;
}

export interface MetadataPerson {
  Name: string;
  Role: string;
  Type: string;
  SortOrder: number | null;
}

export interface MetadataValues {
  Name: string;
  SortName: string;
  Overview: string;
  OriginalTitle: string;
  OfficialRating: string;
  ProductionYear: number | null;
  PremiereDate: string | null;
  CommunityRating: number | null;
  ProviderIds: Record<string, string>;
  Genres: string[];
  Tags: string[];
  Studios: string[];
  People: MetadataPerson[];
  IndexNumber: number | null;
  ParentIndexNumber: number | null;
}

export type MetadataFieldName = keyof MetadataValues;

export interface MetadataItemIdentity {
  Id: string;
  LibraryId: string;
  ParentId: string;
  ParentName: string;
  Name: string;
  Type: string;
  Path: string;
  IsFolder: boolean;
}

export interface MetadataDetail {
  Item: MetadataItemIdentity;
  Revision: string;
  Automatic: MetadataValues;
  Effective: MetadataValues;
  Overrides: Partial<MetadataValues>;
  LockedValues: Partial<MetadataValues>;
  LockedFields: MetadataFieldName[];
  EditableFields: MetadataFieldName[];
  InactiveFields: MetadataFieldName[];
  LastEditedBy: string;
  LastEditedAt: string | null;
}

export interface MetadataUpdateInput {
  Revision: string;
  Overrides: Partial<MetadataValues>;
  LockedFields: MetadataFieldName[];
}

export interface MetadataItemSummary extends MetadataItemIdentity {
  ProductionYear: number | null;
  IndexNumber: number | null;
  ParentIndexNumber: number | null;
  HasOverrides: boolean;
  LockedFieldCount: number;
}

export interface MetadataItemsResponse {
  Library: { Id: string; Name: string; CollectionType: string };
  Items: MetadataItemSummary[];
  TotalRecordCount: number;
  StartIndex: number;
  Limit: number;
}

export interface MetadataItemsQuery {
  SearchTerm?: string;
  Types?: string[];
  StartIndex?: number;
  Limit?: number;
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

export type LoginSessionKind = "admin" | "emby";
export type LoginSessionStatus = "active" | "revoked" | "disabled" | "expired";

export interface LoginSession {
  Id: string;
  UserId: string;
  UserName: string;
  UserIsAdministrator: boolean;
  UserIsDisabled: boolean;
  Kind: LoginSessionKind;
  Client: string;
  DeviceId: string;
  DeviceName: string;
  ApplicationVersion: string;
  CreatedAt: string;
  LastSeenAt: string;
  ExpiresAt: string;
  RevokedAt: string | null;
  Status: LoginSessionStatus;
  IsCurrent: boolean;
}

export interface SessionsQuery {
  UserId?: string;
  Kind?: LoginSessionKind;
  Status?: LoginSessionStatus | "all";
  DeviceId?: string;
  SearchTerm?: string;
  StartIndex?: number;
  Limit?: number;
}

export interface SessionsResponse {
  Items: LoginSession[];
  TotalRecordCount: number;
  StartIndex: number;
  Limit: number;
}

export interface RevokeSessionResponse {
  SessionId: string;
  UserId: string;
  Kind: LoginSessionKind;
  RevokedAt: string;
  CurrentSessionRevoked: boolean;
}

export interface Device {
  Id: string;
  Revision: string;
  ReportedDeviceId: string;
  Name: string;
  ReportedName: string;
  CustomName: string | null;
  AppName: string;
  AppVersion: string;
  LastUserId: string | null;
  LastUserName: string | null;
  CreatedAt: string;
  LastSeenAt: string;
  IpAddress: string;
  ActiveLoginCount: number;
}

export interface DevicesQuery {
  SearchTerm?: string;
  StartIndex?: number;
  Limit?: number;
}

export interface DevicesResponse {
  Items: Device[];
  TotalRecordCount: number;
  StartIndex: number;
  Limit: number;
}

export interface UpdateDeviceOptionsInput {
  Revision: string;
  CustomName: string;
}

export interface DeleteDeviceInput {
  Revision: string;
}

export interface DeleteDeviceResponse {
  Id: string;
  DeletedAt: string;
  RevokedLoginCount: number;
}

export type ApplicationKeyStatus = "active" | "revoked";

export interface ApplicationKey {
  Id: string;
  AppName: string;
  CreatedAt: string;
  LastUsedAt: string | null;
  RevokedAt: string | null;
  CreatedBy: string | null;
  IPAddress: string;
  Status: ApplicationKeyStatus;
}

export interface ApplicationKeysQuery {
  SearchTerm?: string;
  IncludeRevoked?: boolean;
  StartIndex?: number;
  Limit?: number;
}

export interface ApplicationKeysResponse {
  Items: ApplicationKey[];
  TotalRecordCount: number;
  StartIndex: number;
  Limit: number;
}

export interface CreateApplicationKeyInput {
  AppName: string;
}

export interface CreateApplicationKeyResponse {
  Key: ApplicationKey;
  AccessToken: string;
}

export interface RevealApplicationKeyResponse {
  Id: string;
  AccessToken: string;
}

export interface RevokeApplicationKeyResponse {
  Id: string;
  RevokedAt: string;
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
    ApplicationKeys: boolean;
  };
}

export const activityActions = [
  "user.created", "user.updated", "user.password_reset", "session.login", "session.revoked",
  "application_key.created", "application_key.revealed", "application_key.revoked", "device.updated", "device.removed",
  "library.created", "library.removed", "scan.requested", "scan.cancel_requested", "scan.finished", "metadata.updated",
  "settings.updated", "task.admitted", "task.cancel_requested", "task.finished", "task.schedule_updated",
] as const;
export const activitySeverities = ["Info", "Debug", "Warn", "Error", "Fatal"] as const;
export type ActivityAction = typeof activityActions[number];
export type ActivitySeverity = typeof activitySeverities[number];
export type ActivityResourceKind = "user" | "session" | "application_key" | "device" | "library" | "scan" | "item" | "settings" | "task" | "task_run";

export interface ActivityEntry {
  Id: string;
  Date: string;
  Action: ActivityAction;
  Severity: ActivitySeverity;
  Source: "native" | "emby" | "system";
  Actor: { Kind: "user" | "application_key" | "system"; Id: string | null; Name: string | null };
  Resource: { Kind: ActivityResourceKind; Id: string };
  Revision: string | null;
  Count: string;
  State: "completed" | "failed" | "cancelled" | "interrupted" | null;
  ChangedFields: string[];
  Name: string;
  Overview: string;
}

export interface ObservabilityPageQuery { StartIndex?: number; Limit?: number }
export interface ActivityQuery extends ObservabilityPageQuery {
  MinDate?: string;
  Severity?: ActivitySeverity;
  Action?: ActivityAction;
  ActorId?: string;
}
export interface ActivityResponse {
  Items: ActivityEntry[];
  TotalRecordCount: number;
  StartIndex: number;
  Limit: number;
  RetentionDays: number;
}
export interface ServerLogFile { Name: string; DateCreated: string; DateModified: string; Size: string }
export interface ServerLogsResponse {
  Items: ServerLogFile[];
  TotalRecordCount: number;
  StartIndex: number;
  Limit: number;
  Status: {
    Healthy: boolean;
    Degraded: boolean;
    Closed: boolean;
    MaxFileBytes: string;
    MaxFiles: number;
    RetentionDays: number;
    MinFreeBytes: string;
    Format: "jsonl";
  };
}
export interface ServerLogLinesResponse {
  Items: string[];
  StartIndex: number;
  NextIndex: number;
  TotalRecordCount: number;
  SnapshotSize: string;
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
  ForceProbe: boolean;
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

export type TaskRunState = "pending" | "running" | "stopping" | "completed" | "failed" | "cancelled" | "interrupted";
export type TaskChildState = "waiting" | "queued" | "running" | "completed" | "failed" | "cancelled" | "unavailable" | "interrupted";
export type TaskTriggerKind = "interval" | "daily" | "weekly" | "startup";

export interface TaskTriggerInput {
  Kind: TaskTriggerKind;
  IntervalTicks?: string | null;
  TimeOfDayTicks?: string | null;
  DayOfWeek?: number | null;
  MaxRuntimeTicks?: string | null;
}

export interface TaskTrigger {
  Id: string;
  Kind: TaskTriggerKind;
  IntervalTicks: string | null;
  TimeOfDayTicks: string | null;
  DayOfWeek: number | null;
  MaxRuntimeTicks: string | null;
  NextFireAt: string | null;
  CalculationError: string;
}

export interface TaskRun {
  Id: string;
  TaskId: string;
  State: TaskRunState;
  Source: string;
  RequestId: string | null;
  CreatedAt: string;
  StartedAt: string | null;
  FinishedAt: string | null;
  StopRequestedAt: string | null;
  StopReason: string;
  ErrorCode: string;
  ErrorMessage: string;
  TotalChildren: number;
  TerminalChildren: number;
  CompletedChildren: number;
  FailedChildren: number;
  CancelledChildren: number;
  InterruptedChildren: number;
  UnavailableChildren: number;
  Scanned: number;
  Added: number;
  Updated: number;
}

export interface TaskChild {
  Id: string;
  RunId: string;
  LibraryId: string;
  LibraryName: string;
  Ordinal: number;
  State: TaskChildState;
  ScanJobId: string | null;
  Scanned: number;
  Added: number;
  Updated: number;
  ErrorCode: string;
  ErrorMessage: string;
  CreatedAt: string;
  StartedAt: string | null;
  FinishedAt: string | null;
}

export interface TaskDefinition {
  Id: string;
  Key: string;
  Name: string;
  Description: string;
  Category: string;
  IsHidden: boolean;
  Enabled: boolean;
  Revision: string;
  ScheduleTimezone: string;
  Triggers: TaskTrigger[];
  CurrentRun: TaskRun | null;
  LastRun: TaskRun | null;
  NextRunAt: string | null;
}

export interface TaskDefinitionsResponse { Items: TaskDefinition[]; TotalRecordCount: number }
export interface TaskDefinitionResponse { Task: TaskDefinition }
export interface TaskRunResponse { Run: TaskRun }
export interface TaskAdmissionResponse extends TaskRunResponse { Admitted: boolean }
export interface TaskPageQuery { StartIndex?: number; Limit?: number }
export interface TaskPage<T> { Items: T[]; TotalRecordCount: number; StartIndex: number; Limit: number }
export type TaskRunsResponse = TaskPage<TaskRun>;
export interface TaskRunDetail extends TaskRunResponse { Children: TaskPage<TaskChild> }
export interface TaskScheduleInput { ScheduleTimezone: string; Triggers: TaskTriggerInput[] }
export interface TaskScheduleUpdate extends TaskScheduleInput { Revision: string }
export interface TaskSchedulePreview {
  ServerTime: string;
  Items: { Index: number; Occurrences: string[]; Event: string | null }[];
}

export interface StorageRootsResponse {
  Items: {
    Path: string;
    Available: boolean;
  }[];
  Configured: boolean;
}

export interface SettingsValues {
  ServerName: string;
  MaxBitrate: number;
  MaxWidth: number;
  MaxHeight: number;
  MaxAudioChannels: number;
}

export type SettingsField = keyof SettingsValues;
export type SettingsOverrides = { [Field in SettingsField]: SettingsValues[Field] | null };
export type ServerNameMode = "deployment" | "custom" | "empty" | "unset";
export type SettingsResetField = SettingsField | "TranscodingMaxWidth";
export interface SettingsEncoding {
  TranscodingMaxWidth: number;
}

export interface SettingsDeployment {
  HostName: string;
  TranscodingEnabled: boolean;
  HardwareDecoder: string;
  HardwareEncoder: string;
  Threads: number;
  MaxJobs: number;
  MaxUserJobs: number;
  MaxSessionJobs: number;
}

export interface ServerSettings {
  Revision: string;
  Defaults: SettingsValues;
  Overrides: SettingsOverrides;
  Effective: SettingsValues;
  Sources: Record<SettingsField, "deployment" | "database">;
  UpdatedAt: string;
  Deployment: SettingsDeployment;
  ServerNameMode: ServerNameMode;
  Encoding: SettingsEncoding;
}

export interface SettingsUpdateInput {
  Revision: string;
  Overrides: SettingsOverrides;
  ServerNameMode: ServerNameMode;
  Encoding: SettingsEncoding;
}
export interface SettingsResetInput { Revision: string; Fields: SettingsResetField[] }

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
  method?: "GET" | "POST" | "PUT" | "DELETE";
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
  method: "POST" | "PUT" | "DELETE",
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

function validateManagedUser(result: ManagedUserResponse): void {
  if (!isRecord(result)) throw invalidResponse();
  const user = result.User;
  if (!isRecord(user) || !nonemptyString(user.Id) || typeof user.Name !== "string"
    || typeof user.CreatedAt !== "string" || typeof user.Revision !== "string"
    || !/^[1-9]\d*$/.test(user.Revision) || user.Revision.length > 19
    || (user.Revision.length === 19 && user.Revision > "9223372036854775807")
    || ![user.IsAdministrator, user.IsDisabled, user.HasPassword].every((value) => typeof value === "boolean")
    || !isRecord(user.Policy)) throw invalidResponse();
  const policy = user.Policy;
  if (!Array.isArray(policy.EnabledFolders) || !policy.EnabledFolders.every((id) => typeof id === "string")
    || ![policy.EnableAllFolders, policy.EnableMediaPlayback, policy.EnablePlaybackRemuxing,
      policy.EnableAudioPlaybackTranscoding, policy.EnableVideoPlaybackTranscoding]
      .every((value) => typeof value === "boolean")) throw invalidResponse();
}

function validSessionTimestamp(value: unknown): value is string {
  return typeof value === "string"
    && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(value)
    && value.slice(0, 4) !== "0000" && Number.isFinite(Date.parse(value));
}

function validateSessions(result: SessionsResponse, startIndex: number, limit: number): void {
  if (!isRecord(result) || !Array.isArray(result.Items)
    || !Number.isSafeInteger(result.TotalRecordCount) || result.TotalRecordCount < 0
    || result.StartIndex !== startIndex || result.Limit !== limit
    || result.Items.length > limit || result.Items.length > result.TotalRecordCount) throw invalidResponse();
  const ids = new Set<string>();
  for (const session of result.Items) {
    if (!isRecord(session) || !nonemptyString(session.Id) || !nonemptyString(session.UserId)
      || ids.has(session.Id)
      || ![session.UserName, session.Client, session.DeviceId, session.DeviceName, session.ApplicationVersion]
        .every((value) => typeof value === "string")
      || ![session.UserIsAdministrator, session.UserIsDisabled, session.IsCurrent]
        .every((value) => typeof value === "boolean")
      || !["admin", "emby"].includes(session.Kind)
      || !["active", "revoked", "disabled", "expired"].includes(session.Status)
      || ![session.CreatedAt, session.LastSeenAt, session.ExpiresAt].every(validSessionTimestamp)
      || (session.RevokedAt !== null && !validSessionTimestamp(session.RevokedAt))
      || (session.Status === "revoked") !== (session.RevokedAt !== null)
      || (session.IsCurrent && session.Kind !== "admin")) throw invalidResponse();
    ids.add(session.Id);
  }
}

const deviceFields = new Set([
  "Id", "Revision", "ReportedDeviceId", "Name", "ReportedName", "CustomName", "AppName",
  "AppVersion", "LastUserId", "LastUserName", "CreatedAt", "LastSeenAt", "IpAddress", "ActiveLoginCount",
]);
const devicesResponseFields = new Set(["Items", "TotalRecordCount", "StartIndex", "Limit"]);

function validDeviceNumber(value: unknown): value is string {
  return typeof value === "string" && /^[1-9]\d*$/.test(value);
}

function validDevice(value: unknown): value is Device {
  return isRecord(value) && Object.keys(value).length === deviceFields.size
    && Object.keys(value).every((field) => deviceFields.has(field))
    && validDeviceNumber(value.Id) && validDeviceNumber(value.Revision)
    && Boolean(nonemptyString(value.ReportedDeviceId)) && Boolean(nonemptyString(value.Name))
    && [value.ReportedName, value.AppName, value.AppVersion, value.IpAddress].every((field) => typeof field === "string")
    && (value.CustomName === null || (typeof value.CustomName === "string" && value.CustomName.length > 0
      && value.CustomName === value.CustomName.trim() && value.Name === value.CustomName
      && !/[\u0000-\u001f\u007f-\u009f]/u.test(value.CustomName)
      && new TextEncoder().encode(value.CustomName).length <= 256))
    && (value.LastUserId === null || Boolean(nonemptyString(value.LastUserId)))
    && (value.LastUserName === null || typeof value.LastUserName === "string")
    && validSessionTimestamp(value.CreatedAt) && validSessionTimestamp(value.LastSeenAt)
    && typeof value.ActiveLoginCount === "number" && Number.isSafeInteger(value.ActiveLoginCount)
    && value.ActiveLoginCount >= 0;
}

function validateDevices(result: DevicesResponse, startIndex: number, limit: number): void {
  if (!isRecord(result) || Object.keys(result).length !== devicesResponseFields.size
    || !Object.keys(result).every((field) => devicesResponseFields.has(field))
    || !Array.isArray(result.Items) || !Number.isSafeInteger(result.TotalRecordCount) || result.TotalRecordCount < 0
    || result.StartIndex !== startIndex || result.Limit !== limit
    || result.Items.length !== Math.min(limit, Math.max(0, result.TotalRecordCount - startIndex))) throw invalidResponse();
  const ids = new Set<string>();
  for (const device of result.Items) {
    if (!validDevice(device) || ids.has(device.Id)) throw invalidResponse();
    ids.add(device.Id);
  }
}

const applicationKeyFields = new Set([
  "Id", "AppName", "CreatedAt", "LastUsedAt", "RevokedAt", "CreatedBy", "IPAddress", "Status",
]);
const applicationKeysResponseFields = new Set(["Items", "TotalRecordCount", "StartIndex", "Limit"]);

function validApplicationKeyId(value: unknown): value is string {
  return typeof value === "string" && /^[1-9]\d*$/.test(value)
    && value.length <= 19 && (value.length < 19 || value <= "9223372036854775807");
}

function validApplicationKey(value: unknown): value is ApplicationKey {
  return isRecord(value) && Object.keys(value).every((field) => applicationKeyFields.has(field))
    && validApplicationKeyId(value.Id) && Boolean(nonemptyString(value.AppName))
    && validSessionTimestamp(value.CreatedAt)
    && (value.LastUsedAt === null || validSessionTimestamp(value.LastUsedAt))
    && (value.RevokedAt === null || validSessionTimestamp(value.RevokedAt))
    && (value.CreatedBy === null || Boolean(nonemptyString(value.CreatedBy)))
    && typeof value.IPAddress === "string"
    && (value.Status === "active" || value.Status === "revoked")
    && (value.Status === "revoked") === (value.RevokedAt !== null);
}

function validateApplicationKeys(result: ApplicationKeysResponse, startIndex: number, limit: number, includeRevoked: boolean): void {
  if (!isRecord(result) || !Object.keys(result).every((field) => applicationKeysResponseFields.has(field))
    || !Array.isArray(result.Items) || !Number.isSafeInteger(result.TotalRecordCount) || result.TotalRecordCount < 0
    || result.StartIndex !== startIndex || result.Limit !== limit
    || result.Items.length > limit || result.Items.length > Math.max(0, result.TotalRecordCount - startIndex)) throw invalidResponse();
  const ids = new Set<string>();
  for (const key of result.Items) {
    if (!validApplicationKey(key) || ids.has(key.Id) || (!includeRevoked && key.Status === "revoked")) throw invalidResponse();
    ids.add(key.Id);
  }
}

async function mutateUser(
  path: string,
  method: "POST" | "PUT",
  input: UpdateUserInput | ResetUserPasswordInput,
  options: RequestOptions,
): Promise<UserMutationResponse> {
  const revision = sessionRevision;
  const result = await mutate<UserMutationResponse>(path, method, input, options);
  if (revision !== sessionRevision) throw sessionChanged();
  validateManagedUser(result);
  if (typeof result.CurrentSessionRevoked !== "boolean") throw invalidResponse();
  if (result.CurrentSessionRevoked) expireSession(revision);
  return result;
}

const metadataFieldNames: MetadataFieldName[] = [
  "Name", "SortName", "Overview", "OriginalTitle", "OfficialRating", "ProductionYear",
  "PremiereDate", "CommunityRating", "ProviderIds", "Genres", "Tags", "Studios", "People",
  "IndexNumber", "ParentIndexNumber",
];
const metadataFieldSet = new Set<string>(metadataFieldNames);

function validMetadataInteger(value: unknown, minimum = 0, maximum = 2147483647): boolean {
  return value === null || (typeof value === "number" && Number.isInteger(value) && value >= minimum && value <= maximum);
}

function validMetadataTimestamp(value: unknown): boolean {
  return value === null || (typeof value === "string"
    && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(value)
    && value.slice(0, 4) !== "0000" && Number.isFinite(Date.parse(value)));
}

function validMetadataValue(field: string, value: unknown): boolean {
  switch (field) {
    case "Name": case "SortName": case "Overview": case "OriginalTitle": case "OfficialRating":
      return typeof value === "string";
    case "ProductionYear":
      return validMetadataInteger(value, 1, 9999);
    case "IndexNumber": case "ParentIndexNumber":
      return validMetadataInteger(value);
    case "PremiereDate":
      return validMetadataTimestamp(value);
    case "CommunityRating":
      return value === null || (typeof value === "number" && Number.isFinite(value) && value >= 0 && value <= 10);
    case "ProviderIds":
      return isRecord(value) && Object.values(value).every((identifier) => typeof identifier === "string");
    case "Genres": case "Tags": case "Studios":
      return Array.isArray(value) && value.every((name) => typeof name === "string");
    case "People":
      return Array.isArray(value) && value.every((person) => isRecord(person)
        && typeof person.Name === "string" && typeof person.Role === "string" && typeof person.Type === "string"
        && validMetadataInteger(person.SortOrder));
    default:
      return false;
  }
}

function validMetadataValues(value: unknown, sparse: boolean): boolean {
  return isRecord(value)
    && Object.entries(value).every(([field, entry]) => metadataFieldSet.has(field) && validMetadataValue(field, entry))
    && (sparse || metadataFieldNames.every((field) => Object.hasOwn(value, field)));
}

function validMetadataFields(value: unknown): value is MetadataFieldName[] {
  return Array.isArray(value) && value.every((field) => typeof field === "string" && metadataFieldSet.has(field))
    && new Set(value).size === value.length;
}

function validMetadataIdentity(value: unknown): value is MetadataItemIdentity {
  return isRecord(value) && Boolean(nonemptyString(value.Id)) && Boolean(nonemptyString(value.LibraryId))
    && [value.ParentId, value.ParentName, value.Name, value.Type, value.Path].every((entry) => typeof entry === "string")
    && typeof value.IsFolder === "boolean";
}

function validateMetadataDetail(value: MetadataDetail, itemId: string): void {
  if (!isRecord(value) || !validMetadataIdentity(value.Item) || value.Item.Id !== itemId
    || typeof value.Revision !== "string" || !/^[1-9]\d*$/.test(value.Revision)
    || value.Revision.length > 19 || (value.Revision.length === 19 && value.Revision > "9223372036854775807")
    || !validMetadataValues(value.Automatic, false) || !validMetadataValues(value.Effective, false)
    || !validMetadataValues(value.Overrides, true) || !validMetadataValues(value.LockedValues, true)
    || !validMetadataFields(value.LockedFields) || !validMetadataFields(value.EditableFields) || !validMetadataFields(value.InactiveFields)
    || typeof value.LastEditedBy !== "string" || !validMetadataTimestamp(value.LastEditedAt)) throw invalidResponse();
  if (value.LockedFields.length !== Object.keys(value.LockedValues).length
    || !value.LockedFields.every((field) => Object.hasOwn(value.LockedValues, field))) throw invalidResponse();
}

function validateMetadataItems(value: MetadataItemsResponse, libraryId: string, startIndex: number, limit: number): void {
  if (!isRecord(value) || !isRecord(value.Library) || value.Library.Id !== libraryId
    || typeof value.Library.Name !== "string" || typeof value.Library.CollectionType !== "string"
    || !Array.isArray(value.Items) || !Number.isSafeInteger(value.TotalRecordCount) || value.TotalRecordCount < 0
    || value.StartIndex !== startIndex || value.Limit !== limit || value.Items.length > limit
    || value.Items.length > value.TotalRecordCount) throw invalidResponse();
  const ids = new Set<string>();
  for (const item of value.Items) {
    if (!validMetadataIdentity(item) || item.LibraryId !== libraryId || ids.has(item.Id)
      || !validMetadataInteger(item.ProductionYear, 1, 9999)
      || !validMetadataInteger(item.IndexNumber) || !validMetadataInteger(item.ParentIndexNumber)
      || typeof item.HasOverrides !== "boolean" || !Number.isInteger(item.LockedFieldCount)
      || item.LockedFieldCount < 0 || item.LockedFieldCount > metadataFieldNames.length) throw invalidResponse();
    ids.add(item.Id);
  }
}

function validTaskCount(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
}

function validTaskTimestamp(value: unknown): boolean {
  return value === null || validSessionTimestamp(value);
}

function validTaskTicks(value: unknown): boolean {
  return value === null || (typeof value === "string" && /^(0|[1-9]\d*)$/.test(value)
    && value.length <= 17 && BigInt(value) <= 92233720368547758n);
}

function validTaskRun(value: unknown): value is TaskRun {
  if (!isRecord(value) || !nonemptyString(value.Id) || !nonemptyString(value.TaskId)
    || typeof value.State !== "string" || !["pending", "running", "stopping", "completed", "failed", "cancelled", "interrupted"].includes(value.State)
    || ![value.Source, value.StopReason, value.ErrorCode, value.ErrorMessage].every((entry) => typeof entry === "string")
    || (value.RequestId !== null && typeof value.RequestId !== "string")
    || !validSessionTimestamp(value.CreatedAt) || ![value.StartedAt, value.FinishedAt, value.StopRequestedAt].every(validTaskTimestamp)
    || ![value.TotalChildren, value.TerminalChildren, value.CompletedChildren, value.FailedChildren,
      value.CancelledChildren, value.InterruptedChildren, value.UnavailableChildren, value.Scanned, value.Added, value.Updated].every(validTaskCount)) return false;
  return (value.TerminalChildren as number) <= (value.TotalChildren as number);
}

function validTaskDefinition(value: unknown): value is TaskDefinition {
  if (!isRecord(value) || !nonemptyString(value.Id) || !nonemptyString(value.Key)
    || ![value.Name, value.Description, value.Category, value.ScheduleTimezone].every((entry) => typeof entry === "string")
    || typeof value.Revision !== "string" || !/^[1-9]\d*$/.test(value.Revision) || value.Revision.length > 19
    || BigInt(value.Revision) > 9223372036854775807n || typeof value.IsHidden !== "boolean" || typeof value.Enabled !== "boolean"
    || !validTaskTimestamp(value.NextRunAt) || !Array.isArray(value.Triggers) || value.Triggers.length > 32
    || (value.CurrentRun !== null && (!validTaskRun(value.CurrentRun) || value.CurrentRun.TaskId !== value.Id))
    || (value.LastRun !== null && (!validTaskRun(value.LastRun) || value.LastRun.TaskId !== value.Id))) return false;
  const ids = new Set<string>();
  return value.Triggers.every((trigger: unknown) => {
    if (!isRecord(trigger) || !nonemptyString(trigger.Id) || ids.has(trigger.Id as string)
      || typeof trigger.Kind !== "string" || !["interval", "daily", "weekly", "startup"].includes(trigger.Kind)
      || ![trigger.IntervalTicks, trigger.TimeOfDayTicks, trigger.MaxRuntimeTicks].every(validTaskTicks)
      || (trigger.DayOfWeek !== null && (!Number.isInteger(trigger.DayOfWeek) || (trigger.DayOfWeek as number) < 0 || (trigger.DayOfWeek as number) > 6))
      || !validTaskTimestamp(trigger.NextFireAt) || typeof trigger.CalculationError !== "string") return false;
    ids.add(trigger.Id as string);
    return true;
  });
}

function validTaskChild(value: unknown): value is TaskChild {
  return isRecord(value) && Boolean(nonemptyString(value.Id)) && Boolean(nonemptyString(value.RunId))
    && [value.LibraryId, value.LibraryName, value.ErrorCode, value.ErrorMessage].every((entry) => typeof entry === "string")
    && typeof value.State === "string" && ["waiting", "queued", "running", "completed", "failed", "cancelled", "unavailable", "interrupted"].includes(value.State)
    && (value.ScanJobId === null || typeof value.ScanJobId === "string")
    && [value.Ordinal, value.Scanned, value.Added, value.Updated].every(validTaskCount)
    && validSessionTimestamp(value.CreatedAt) && [value.StartedAt, value.FinishedAt].every(validTaskTimestamp);
}

function validateTaskPage<T extends { Id: string }>(value: TaskPage<T>, query: TaskPageQuery, check: (item: unknown) => item is T): void {
  if (!isRecord(value) || !Array.isArray(value.Items) || !validTaskCount(value.TotalRecordCount)
    || value.StartIndex !== (query.StartIndex ?? 0) || value.Limit !== (query.Limit ?? 50)
    || value.Items.length > value.Limit || value.Items.length > value.TotalRecordCount
    || !value.Items.every(check) || new Set(value.Items.map((item) => item.Id)).size !== value.Items.length) throw invalidResponse();
}

function taskPageParameters(query: TaskPageQuery): URLSearchParams {
  return new URLSearchParams({ StartIndex: String(query.StartIndex ?? 0), Limit: String(query.Limit ?? 50) });
}

const managedSettingFields: SettingsField[] = ["ServerName", "MaxBitrate", "MaxWidth", "MaxHeight", "MaxAudioChannels"];

function validSettingsRecord(value: unknown, fields: readonly string[]): value is Record<string, unknown> {
  return isRecord(value) && Object.keys(value).length === fields.length
    && Object.keys(value).every((field) => fields.includes(field));
}

function validSettingsText(value: unknown): value is string {
  return typeof value === "string" && value.length > 0 && !value.includes("\0")
    && !/[\uD800-\uDFFF]/u.test(value);
}

function validSettingValue(field: SettingsField, value: unknown): boolean {
  if (field === "ServerName") return validSettingsText(value) && /[^\p{White_Space}]/u.test(value)
    && new TextEncoder().encode(value).length <= 128;
  const maximum = field === "MaxBitrate" ? 1000000000 : field === "MaxAudioChannels" ? 8 : 8192;
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 1 && value <= maximum;
}

function validateSettings(value: ServerSettings): void {
  if (!validSettingsRecord(value, ["Revision", "Defaults", "Overrides", "Effective", "Sources", "UpdatedAt", "Deployment", "ServerNameMode", "Encoding"])
    || typeof value.Revision !== "string" || !/^[1-9]\d*$/.test(value.Revision)
    || value.Revision.length > 19 || BigInt(value.Revision) > 9223372036854775807n
    || !validSessionTimestamp(value.UpdatedAt)
    || !validSettingsRecord(value.Defaults, managedSettingFields)
    || !validSettingsRecord(value.Overrides, managedSettingFields)
    || !validSettingsRecord(value.Effective, managedSettingFields)
    || !validSettingsRecord(value.Sources, managedSettingFields)
    || !validSettingsRecord(value.Deployment, ["HostName", "TranscodingEnabled", "HardwareDecoder", "HardwareEncoder", "Threads", "MaxJobs", "MaxUserJobs", "MaxSessionJobs"])
    || !validSettingsRecord(value.Encoding, ["TranscodingMaxWidth"])) throw invalidResponse();
  for (const field of managedSettingFields) {
    if (field === "ServerName") continue;
    const override = value.Overrides[field];
    if (!validSettingValue(field, value.Defaults[field]) || !validSettingValue(field, value.Effective[field])
      || (override !== null && !validSettingValue(field, override))
      || value.Effective[field] !== (override ?? value.Defaults[field])
      || value.Sources[field] !== (override === null ? "deployment" : "database")) throw invalidResponse();
  }
  const deployment = value.Deployment;
  if (!validSettingsText(deployment.HostName) || typeof deployment.TranscodingEnabled !== "boolean"
    || typeof deployment.HardwareDecoder !== "string" || typeof deployment.HardwareEncoder !== "string"
    || ![deployment.Threads, deployment.MaxJobs, deployment.MaxUserJobs, deployment.MaxSessionJobs]
      .every((count) => typeof count === "number" && Number.isSafeInteger(count) && count >= 0)) throw invalidResponse();
  if (!validSettingValue("ServerName", value.Defaults.ServerName)) throw invalidResponse();
  const name = value.Overrides.ServerName;
  switch (value.ServerNameMode) {
    case "deployment":
      if (name !== null || value.Effective.ServerName !== value.Defaults.ServerName
        || value.Sources.ServerName !== "deployment") throw invalidResponse();
      break;
    case "custom":
      if (!validSettingValue("ServerName", name) || value.Effective.ServerName !== name
        || value.Sources.ServerName !== "database") throw invalidResponse();
      break;
    case "empty":
      if (name !== "" || value.Effective.ServerName !== deployment.HostName
        || value.Sources.ServerName !== "database") throw invalidResponse();
      break;
    case "unset":
      if (name !== null || value.Effective.ServerName !== deployment.HostName
        || value.Sources.ServerName !== "database") throw invalidResponse();
      break;
    default:
      throw invalidResponse();
  }
  const width = value.Encoding.TranscodingMaxWidth;
  if (typeof width !== "number" || !Number.isSafeInteger(width) || width < 0 || width > 8192) throw invalidResponse();
}

function validObservabilityDecimal(value: unknown, positive = false): value is string {
  return typeof value === "string" && (positive ? /^[1-9]\d*$/ : /^(0|[1-9]\d*)$/).test(value);
}

function validLogName(value: unknown): value is string {
  return typeof value === "string" && value.length > 0 && value !== "." && value !== ".."
    && !/[\\/\u0000-\u001f\u007f]/u.test(value);
}

function validActivityEntry(value: unknown): value is ActivityEntry {
  if (!isRecord(value) || !validObservabilityDecimal(value.Id, true) || !validSessionTimestamp(value.Date)
    || typeof value.Action !== "string" || !(activityActions as readonly string[]).includes(value.Action)
    || typeof value.Severity !== "string" || !(activitySeverities as readonly string[]).includes(value.Severity)
    || !["native", "emby", "system"].includes(value.Source as string)
    || !isRecord(value.Actor) || !["user", "application_key", "system"].includes(value.Actor.Kind as string)
    || (value.Actor.Id !== null && !nonemptyString(value.Actor.Id))
    || (value.Actor.Name !== null && typeof value.Actor.Name !== "string")
    || !isRecord(value.Resource) || !nonemptyString(value.Resource.Id)
    || !["user", "session", "application_key", "device", "library", "scan", "item", "settings", "task", "task_run"].includes(value.Resource.Kind as string)
    || (value.Revision !== null && !validObservabilityDecimal(value.Revision, true))
    || !validObservabilityDecimal(value.Count)
    || (value.State !== null && !["completed", "failed", "cancelled", "interrupted"].includes(value.State as string))
    || !Array.isArray(value.ChangedFields) || !value.ChangedFields.every((field) => typeof field === "string")
    || typeof value.Name !== "string" || typeof value.Overview !== "string") return false;
  return Object.keys(value).every((field) => ["Id", "Date", "Action", "Severity", "Source", "Actor", "Resource", "Revision", "Count", "State", "ChangedFields", "Name", "Overview"].includes(field))
    && Object.keys(value.Actor).every((field) => ["Kind", "Id", "Name"].includes(field))
    && Object.keys(value.Resource).every((field) => ["Kind", "Id"].includes(field));
}

function validateObservabilityPage(value: { Items: unknown[]; TotalRecordCount: number; StartIndex: number; Limit: number }, query: ObservabilityPageQuery): void {
  const startIndex = query.StartIndex ?? 0;
  const limit = query.Limit ?? 50;
  if (!Array.isArray(value.Items) || !validTaskCount(value.TotalRecordCount)
    || value.StartIndex !== startIndex || value.Limit !== limit
    || value.Items.length !== Math.min(limit, Math.max(0, value.TotalRecordCount - startIndex))) throw invalidResponse();
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

  async getActivity(query: ActivityQuery = {}, options: RequestOptions = {}): Promise<ActivityResponse> {
    const revision = sessionRevision;
    const parameters = taskPageParameters(query);
    for (const field of ["MinDate", "Severity", "Action", "ActorId"] as const) {
      const value = query[field];
      if (value) parameters.set(field, value);
    }
    const result = await request<ActivityResponse>(`/activity?${parameters}`, options);
    if (revision !== sessionRevision) throw sessionChanged();
    validateObservabilityPage(result, query);
    if (!validTaskCount(result.RetentionDays) || !result.Items.every(validActivityEntry)
      || new Set(result.Items.map((item) => item.Id)).size !== result.Items.length
      || !Object.keys(result).every((field) => ["Items", "TotalRecordCount", "StartIndex", "Limit", "RetentionDays"].includes(field))) throw invalidResponse();
    return result;
  },

  async getServerLogs(query: ObservabilityPageQuery = {}, options: RequestOptions = {}): Promise<ServerLogsResponse> {
    const revision = sessionRevision;
    const result = await request<ServerLogsResponse>(`/logs?${taskPageParameters(query)}`, options);
    if (revision !== sessionRevision) throw sessionChanged();
    validateObservabilityPage(result, query);
    const status = result.Status;
    if (!isRecord(status) || ![status.Healthy, status.Degraded, status.Closed].every((value) => typeof value === "boolean")
      || !validObservabilityDecimal(status.MaxFileBytes, true) || !validObservabilityDecimal(status.MinFreeBytes)
      || !validTaskCount(status.MaxFiles) || status.MaxFiles < 1 || !validTaskCount(status.RetentionDays) || status.Format !== "jsonl"
      || !Object.keys(status).every((field) => ["Healthy", "Degraded", "Closed", "MaxFileBytes", "MaxFiles", "RetentionDays", "MinFreeBytes", "Format"].includes(field))
      || !Object.keys(result).every((field) => ["Items", "TotalRecordCount", "StartIndex", "Limit", "Status"].includes(field))) throw invalidResponse();
    const names = new Set<string>();
    for (const file of result.Items) {
      if (!isRecord(file) || !validLogName(file.Name) || names.has(file.Name)
        || !validSessionTimestamp(file.DateCreated) || !validSessionTimestamp(file.DateModified) || !validObservabilityDecimal(file.Size)
        || !Object.keys(file).every((field) => ["Name", "DateCreated", "DateModified", "Size"].includes(field))) throw invalidResponse();
      names.add(file.Name);
    }
    return result;
  },

  async getServerLogLines(name: string, query: ObservabilityPageQuery = {}, options: RequestOptions = {}): Promise<ServerLogLinesResponse> {
    const revision = sessionRevision;
    if (!validLogName(name)) throw new ApiError("The log filename is invalid. Refresh the file list.", { code: "invalid_log_name" });
    const startIndex = query.StartIndex ?? 0;
    const limit = query.Limit ?? 200;
    const parameters = new URLSearchParams({ StartIndex: String(startIndex), Limit: String(limit) });
    const result = await request<ServerLogLinesResponse>(`/logs/${encodeURIComponent(name)}/lines?${parameters}`, options);
    if (revision !== sessionRevision) throw sessionChanged();
    if (!Array.isArray(result.Items) || !result.Items.every((line) => typeof line === "string")
      || !validTaskCount(result.TotalRecordCount) || result.StartIndex !== startIndex
      || result.Items.length > limit || result.Items.length > Math.max(0, result.TotalRecordCount - startIndex)
      || result.NextIndex !== startIndex + result.Items.length || !validObservabilityDecimal(result.SnapshotSize)
      || !Object.keys(result).every((field) => ["Items", "StartIndex", "NextIndex", "TotalRecordCount", "SnapshotSize"].includes(field))) throw invalidResponse();
    return result;
  },

  serverLogDownloadURL(name: string): string {
    if (!validLogName(name)) throw new ApiError("The log filename is invalid. Refresh the file list.", { code: "invalid_log_name" });
    return `${API_ROOT}/logs/${encodeURIComponent(name)}/download`;
  },

  async getSettings(options: RequestOptions = {}): Promise<ServerSettings> {
    const revision = sessionRevision;
    const result = await request<ServerSettings>("/settings", options);
    if (revision !== sessionRevision) throw sessionChanged();
    validateSettings(result);
    return result;
  },

  async updateSettings(input: SettingsUpdateInput, options: RequestOptions = {}): Promise<ServerSettings> {
    const revision = sessionRevision;
    const result = await mutate<ServerSettings>("/settings", "PUT", input, options);
    if (revision !== sessionRevision) throw sessionChanged();
    validateSettings(result);
    return result;
  },

  async resetSettings(input: SettingsResetInput, options: RequestOptions = {}): Promise<ServerSettings> {
    const revision = sessionRevision;
    const result = await mutate<ServerSettings>("/settings/reset", "POST", input, options);
    if (revision !== sessionRevision) throw sessionChanged();
    validateSettings(result);
    return result;
  },

  getUsers(options: RequestOptions = {}): Promise<UsersResponse> {
    return request("/users", options);
  },

  async getSessions(query: SessionsQuery = {}, options: RequestOptions = {}): Promise<SessionsResponse> {
    const revision = sessionRevision;
    const startIndex = query.StartIndex ?? 0;
    const limit = query.Limit ?? 50;
    const parameters = new URLSearchParams({ StartIndex: String(startIndex), Limit: String(limit) });
    for (const field of ["UserId", "Kind", "Status", "DeviceId", "SearchTerm"] as const) {
      const value = query[field];
      if (value) parameters.set(field, value);
    }
    const result = await request<SessionsResponse>(`/sessions?${parameters}`, options);
    if (revision !== sessionRevision) throw sessionChanged();
    validateSessions(result, startIndex, limit);
    return result;
  },

  async revokeSession(sessionId: string, options: RequestOptions = {}): Promise<RevokeSessionResponse> {
    const revision = sessionRevision;
    const result = await mutate<RevokeSessionResponse>(`/sessions/${encodeURIComponent(sessionId)}/revoke`, "POST", {}, options);
    if (revision !== sessionRevision) throw sessionChanged();
    if (!isRecord(result) || result.SessionId !== sessionId || !nonemptyString(result.UserId)
      || !["admin", "emby"].includes(result.Kind) || !validSessionTimestamp(result.RevokedAt)
      || typeof result.CurrentSessionRevoked !== "boolean"
      || (result.CurrentSessionRevoked && result.Kind !== "admin")) throw invalidResponse();
    if (result.CurrentSessionRevoked) expireSession(revision);
    return result;
  },

  async getDevices(query: DevicesQuery = {}, options: RequestOptions = {}): Promise<DevicesResponse> {
    const revision = sessionRevision;
    const startIndex = query.StartIndex ?? 0;
    const limit = query.Limit ?? 25;
    if (!Number.isSafeInteger(startIndex) || startIndex < 0 || startIndex > 2147483647
      || !Number.isSafeInteger(limit) || limit < 1 || limit > 200) {
      throw new ApiError("The device page is invalid.", { code: "invalid_device_page" });
    }
    const parameters = new URLSearchParams({ StartIndex: String(startIndex), Limit: String(limit) });
    if (query.SearchTerm) parameters.set("SearchTerm", query.SearchTerm);
    const result = await request<DevicesResponse>(`/devices?${parameters}`, options);
    if (revision !== sessionRevision) throw sessionChanged();
    validateDevices(result, startIndex, limit);
    return result;
  },

  async updateDeviceOptions(deviceId: string, input: UpdateDeviceOptionsInput, options: RequestOptions = {}): Promise<Device> {
    const revision = sessionRevision;
    if (!validDeviceNumber(deviceId) || !validDeviceNumber(input.Revision)) {
      throw new ApiError("The device ID or revision is invalid. Refresh the device list.", { code: "invalid_device_revision" });
    }
    const result = await mutate<Device>(`/devices/${encodeURIComponent(deviceId)}/options`, "POST", input, options);
    if (revision !== sessionRevision) throw sessionChanged();
    if (!validDevice(result) || result.Id !== deviceId) throw invalidResponse();
    return result;
  },

  async deleteDevice(deviceId: string, input: DeleteDeviceInput, options: RequestOptions = {}): Promise<DeleteDeviceResponse> {
    const revision = sessionRevision;
    if (!validDeviceNumber(deviceId) || !validDeviceNumber(input.Revision)) {
      throw new ApiError("The device ID or revision is invalid. Refresh the device list.", { code: "invalid_device_revision" });
    }
    const result = await mutate<DeleteDeviceResponse>(`/devices/${encodeURIComponent(deviceId)}/delete`, "POST", input, options);
    if (revision !== sessionRevision) throw sessionChanged();
    if (!isRecord(result) || Object.keys(result).length !== 3
      || !Object.keys(result).every((field) => ["Id", "DeletedAt", "RevokedLoginCount"].includes(field))
      || result.Id !== deviceId || !validSessionTimestamp(result.DeletedAt)
      || !Number.isSafeInteger(result.RevokedLoginCount) || result.RevokedLoginCount < 0) throw invalidResponse();
    return result;
  },

  async getApplicationKeys(query: ApplicationKeysQuery = {}, options: RequestOptions = {}): Promise<ApplicationKeysResponse> {
    const revision = sessionRevision;
    const startIndex = query.StartIndex ?? 0;
    const limit = query.Limit ?? 50;
    const includeRevoked = query.IncludeRevoked ?? false;
    const parameters = new URLSearchParams({ StartIndex: String(startIndex), Limit: String(limit), IncludeRevoked: String(includeRevoked) });
    if (query.SearchTerm) parameters.set("SearchTerm", query.SearchTerm);
    const result = await request<ApplicationKeysResponse>(`/api-keys?${parameters}`, options);
    if (revision !== sessionRevision) throw sessionChanged();
    validateApplicationKeys(result, startIndex, limit, includeRevoked);
    return result;
  },

  async createApplicationKey(input: CreateApplicationKeyInput, options: RequestOptions = {}): Promise<CreateApplicationKeyResponse> {
    const revision = sessionRevision;
    const result = await mutate<CreateApplicationKeyResponse>("/api-keys", "POST", input, options);
    if (revision !== sessionRevision) throw sessionChanged();
    if (!isRecord(result) || !validApplicationKey(result.Key) || result.Key.Status !== "active"
      || !nonemptyString(result.AccessToken)) throw invalidResponse();
    return result;
  },

  async revealApplicationKey(keyId: string, options: RequestOptions = {}): Promise<RevealApplicationKeyResponse> {
    const revision = sessionRevision;
    if (!validApplicationKeyId(keyId)) throw new ApiError("The API key ID is invalid.", { code: "invalid_key_id" });
    const result = await mutate<RevealApplicationKeyResponse>(`/api-keys/${encodeURIComponent(keyId)}/reveal`, "POST", {}, options);
    if (revision !== sessionRevision) throw sessionChanged();
    if (!isRecord(result) || result.Id !== keyId || !nonemptyString(result.AccessToken)) throw invalidResponse();
    return result;
  },

  async revokeApplicationKey(keyId: string, options: RequestOptions = {}): Promise<RevokeApplicationKeyResponse> {
    const revision = sessionRevision;
    if (!validApplicationKeyId(keyId)) throw new ApiError("The API key ID is invalid.", { code: "invalid_key_id" });
    const result = await mutate<RevokeApplicationKeyResponse>(`/api-keys/${encodeURIComponent(keyId)}/revoke`, "POST", {}, options);
    if (revision !== sessionRevision) throw sessionChanged();
    if (!isRecord(result) || result.Id !== keyId || !validSessionTimestamp(result.RevokedAt)
      || !Object.keys(result).every((field) => ["Id", "RevokedAt"].includes(field))) throw invalidResponse();
    return result;
  },

  async createUser(input: CreateUserInput, options: RequestOptions = {}): Promise<UserResponse> {
    const result = await mutate<UserResponse>("/users", "POST", input, options);
    if (!isRecord(result) || !isRecord(result.User)
      || !nonemptyString(result.User.Id) || typeof result.User.Name !== "string"
      || typeof result.User.CreatedAt !== "string"
      || ![result.User.IsAdministrator, result.User.IsDisabled, result.User.HasPassword]
        .every((value) => typeof value === "boolean")) throw invalidResponse();
    return result;
  },

  async getUser(userId: string, options: RequestOptions = {}): Promise<ManagedUserResponse> {
    const revision = sessionRevision;
    const result = await request<ManagedUserResponse>(`/users/${encodeURIComponent(userId)}`, options);
    if (revision !== sessionRevision) throw sessionChanged();
    validateManagedUser(result);
    return result;
  },

  updateUser(userId: string, input: UpdateUserInput, options: RequestOptions = {}): Promise<UserMutationResponse> {
    return mutateUser(`/users/${encodeURIComponent(userId)}`, "PUT", input, options);
  },

  resetUserPassword(userId: string, input: ResetUserPasswordInput, options: RequestOptions = {}): Promise<UserMutationResponse> {
    return mutateUser(`/users/${encodeURIComponent(userId)}/password`, "POST", input, options);
  },

  getLibraries(options: RequestOptions = {}): Promise<LibrariesResponse> {
    return request("/libraries", options);
  },

  async getLibraryItems(libraryId: string, query: MetadataItemsQuery = {}, options: RequestOptions = {}): Promise<MetadataItemsResponse> {
    const revision = sessionRevision;
    const startIndex = query.StartIndex ?? 0;
    const limit = query.Limit ?? 50;
    const parameters = new URLSearchParams({ StartIndex: String(startIndex), Limit: String(limit) });
    if (query.SearchTerm) parameters.set("SearchTerm", query.SearchTerm);
    if (query.Types?.length) parameters.set("Types", query.Types.join(","));
    const result = await request<MetadataItemsResponse>(`/libraries/${encodeURIComponent(libraryId)}/items?${parameters}`, options);
    if (revision !== sessionRevision) throw sessionChanged();
    validateMetadataItems(result, libraryId, startIndex, limit);
    return result;
  },

  async getItemMetadata(itemId: string, options: RequestOptions = {}): Promise<MetadataDetail> {
    const revision = sessionRevision;
    const result = await request<MetadataDetail>(`/items/${encodeURIComponent(itemId)}/metadata`, options);
    if (revision !== sessionRevision) throw sessionChanged();
    validateMetadataDetail(result, itemId);
    return result;
  },

  async updateItemMetadata(itemId: string, input: MetadataUpdateInput, options: RequestOptions = {}): Promise<MetadataDetail> {
    const revision = sessionRevision;
    const result = await mutate<MetadataDetail>(`/items/${encodeURIComponent(itemId)}/metadata`, "PUT", input, options);
    if (revision !== sessionRevision) throw sessionChanged();
    validateMetadataDetail(result, itemId);
    return result;
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

  async refreshLibraryMedia(libraryId: string, options: RequestOptions = {}): Promise<JobResponse> {
    const result = await mutate<JobResponse>(`/libraries/${encodeURIComponent(libraryId)}/scan`, "POST", { ForceProbe: true }, options);
    if (!isRecord(result) || !isRecord(result.Job) || !nonemptyString(result.Job.Id)
      || result.Job.LibraryId !== libraryId || result.Job.ForceProbe !== true) throw invalidResponse();
    return result;
  },

  getJobs(options: RequestOptions = {}): Promise<JobsResponse> {
    return request("/jobs", options);
  },

  async getTasks(options: RequestOptions = {}): Promise<TaskDefinitionsResponse> {
    const revision = sessionRevision;
    const result = await request<TaskDefinitionsResponse>("/tasks", options);
    if (revision !== sessionRevision) throw sessionChanged();
    if (!Array.isArray(result.Items) || !validTaskCount(result.TotalRecordCount)
      || result.Items.length !== result.TotalRecordCount || !result.Items.every(validTaskDefinition)
      || new Set(result.Items.map((task) => task.Id)).size !== result.Items.length) throw invalidResponse();
    return result;
  },

  async getTask(taskId: string, options: RequestOptions = {}): Promise<TaskDefinitionResponse> {
    const revision = sessionRevision;
    const result = await request<TaskDefinitionResponse>(`/tasks/${encodeURIComponent(taskId)}`, options);
    if (revision !== sessionRevision) throw sessionChanged();
    if (!validTaskDefinition(result.Task) || result.Task.Id !== taskId) throw invalidResponse();
    return result;
  },

  async startTask(taskId: string, requestId: string, options: RequestOptions = {}): Promise<TaskAdmissionResponse> {
    const revision = sessionRevision;
    const result = await mutate<TaskAdmissionResponse>(`/tasks/${encodeURIComponent(taskId)}/runs`, "POST", { RequestId: requestId }, options);
    if (revision !== sessionRevision) throw sessionChanged();
    if (!validTaskRun(result.Run) || result.Run.TaskId !== taskId || typeof result.Admitted !== "boolean"
      || (result.Admitted && result.Run.RequestId !== requestId)) throw invalidResponse();
    return result;
  },

  async getTaskRuns(taskId: string, query: TaskPageQuery = {}, options: RequestOptions = {}): Promise<TaskRunsResponse> {
    const revision = sessionRevision;
    const result = await request<TaskRunsResponse>(`/tasks/${encodeURIComponent(taskId)}/runs?${taskPageParameters(query)}`, options);
    if (revision !== sessionRevision) throw sessionChanged();
    validateTaskPage(result, query, validTaskRun);
    if (result.Items.some((run) => run.TaskId !== taskId)) throw invalidResponse();
    return result;
  },

  async getTaskRun(runId: string, query: TaskPageQuery = {}, options: RequestOptions = {}): Promise<TaskRunDetail> {
    const revision = sessionRevision;
    const result = await request<TaskRunDetail>(`/task-runs/${encodeURIComponent(runId)}?${taskPageParameters(query)}`, options);
    if (revision !== sessionRevision) throw sessionChanged();
    if (!validTaskRun(result.Run) || result.Run.Id !== runId) throw invalidResponse();
    validateTaskPage(result.Children, query, validTaskChild);
    if (result.Children.Items.some((child) => child.RunId !== runId)) throw invalidResponse();
    return result;
  },

  async cancelTaskRun(runId: string, options: RequestOptions = {}): Promise<TaskRunResponse> {
    const revision = sessionRevision;
    const result = await mutate<TaskRunResponse>(`/task-runs/${encodeURIComponent(runId)}/cancel`, "POST", {}, options);
    if (revision !== sessionRevision) throw sessionChanged();
    if (!validTaskRun(result.Run) || result.Run.Id !== runId) throw invalidResponse();
    return result;
  },

  async updateTaskSchedule(taskId: string, input: TaskScheduleUpdate, options: RequestOptions = {}): Promise<TaskDefinitionResponse> {
    const revision = sessionRevision;
    const result = await mutate<TaskDefinitionResponse>(`/tasks/${encodeURIComponent(taskId)}/triggers`, "PUT", input, options);
    if (revision !== sessionRevision) throw sessionChanged();
    if (!validTaskDefinition(result.Task) || result.Task.Id !== taskId) throw invalidResponse();
    return result;
  },

  async previewTaskSchedule(taskId: string, input: TaskScheduleInput, options: RequestOptions = {}): Promise<TaskSchedulePreview> {
    const revision = sessionRevision;
    const result = await mutate<TaskSchedulePreview>(`/tasks/${encodeURIComponent(taskId)}/triggers/preview`, "POST", input, options);
    if (revision !== sessionRevision) throw sessionChanged();
    if (!validSessionTimestamp(result.ServerTime) || !Array.isArray(result.Items) || result.Items.length !== input.Triggers.length
      || !result.Items.every((item, index) => isRecord(item) && item.Index === index && Array.isArray(item.Occurrences)
        && item.Occurrences.every(validSessionTimestamp) && (item.Event === null || typeof item.Event === "string"))) throw invalidResponse();
    return result;
  },

  cancelJob(jobId: string, options: RequestOptions = {}): Promise<JobResponse> {
    return mutate(`/jobs/${encodeURIComponent(jobId)}/cancel`, "POST", undefined, options);
  },

  getStorageRoots(options: RequestOptions = {}): Promise<StorageRootsResponse> {
    return request("/storage/roots", options);
  },
};

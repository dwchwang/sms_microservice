import axios, { AxiosError, type AxiosRequestConfig } from "axios";
import { tokenStorage } from "@/store/auth";
import type { ApiErrorEnvelope, FieldError, TokenResponse } from "./types";

export const API_BASE_URL =
  process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080/api/v1";

export const api = axios.create({
  baseURL: API_BASE_URL,
  headers: { "Content-Type": "application/json" },
});

/** Normalized error thrown to callers — carries HTTP status + field errors. */
export class ApiError extends Error {
  status: number;
  fieldErrors: FieldError[];
  constructor(message: string, status: number, fieldErrors: FieldError[] = []) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.fieldErrors = fieldErrors;
  }
}

// ── Request: attach bearer token ──
api.interceptors.request.use((config) => {
  const token = tokenStorage.getAccess();
  if (token) config.headers.Authorization = `Bearer ${token}`;
  return config;
});

// ── Response: single-flight refresh on 401, then normalize errors ──
let refreshing: Promise<string | null> | null = null;

async function doRefresh(): Promise<string | null> {
  const refresh = tokenStorage.getRefresh();
  if (!refresh) return null;
  try {
    const res = await axios.post<{ data: TokenResponse }>(
      `${API_BASE_URL}/auth/refresh`,
      { refresh_token: refresh },
      { headers: { "Content-Type": "application/json" } },
    );
    const t = res.data.data;
    tokenStorage.set(t.access_token, t.refresh_token);
    return t.access_token;
  } catch {
    tokenStorage.clear();
    return null;
  }
}

api.interceptors.response.use(
  (res) => res,
  async (error: AxiosError<ApiErrorEnvelope>) => {
    const original = error.config as (AxiosRequestConfig & { _retried?: boolean }) | undefined;
    const status = error.response?.status ?? 0;
    const isRefreshCall = original?.url?.includes("/auth/refresh");

    if (status === 401 && original && !original._retried && !isRefreshCall) {
      original._retried = true;
      refreshing = refreshing ?? doRefresh();
      const newToken = await refreshing;
      refreshing = null;
      if (newToken) {
        original.headers = { ...original.headers, Authorization: `Bearer ${newToken}` };
        return api(original);
      }
      // Refresh failed → bounce to login (client only)
      if (typeof window !== "undefined") {
        tokenStorage.clear();
        if (!window.location.pathname.startsWith("/login")) {
          window.location.href = "/login";
        }
      }
    }

    const body = error.response?.data;
    throw new ApiError(
      body?.message ?? error.message ?? "Request failed",
      status,
      body?.errors ?? [],
    );
  },
);

/** Unwrap the standard envelope: response.data.data */
export function unwrap<T>(payload: { data: T }): T {
  return payload.data;
}

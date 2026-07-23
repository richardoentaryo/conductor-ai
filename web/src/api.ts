// api.ts is the single choke point for talking to the Conductor gateway. It
// attaches the operator's API key (persisted in localStorage) as a Bearer token
// on every /v1 call. When no key is stored the header is omitted entirely — the
// gateway is a pass-through when it runs keyless, so an empty key must not send
// an empty Authorization header that would look like a failed auth attempt.

import type { ConfigSummary, Trace, Workflow, WorkflowRun } from "./types";

const API_KEY_STORAGE = "conductor.apiKey";

export function getApiKey(): string {
  return localStorage.getItem(API_KEY_STORAGE) ?? "";
}

export function setApiKey(key: string): void {
  if (key) {
    localStorage.setItem(API_KEY_STORAGE, key);
  } else {
    localStorage.removeItem(API_KEY_STORAGE);
  }
}

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

async function apiGet<T>(path: string): Promise<T> {
  const headers: Record<string, string> = {};
  const key = getApiKey();
  if (key) {
    headers.Authorization = `Bearer ${key}`;
  }

  let res: Response;
  try {
    res = await fetch(path, { headers });
  } catch {
    throw new ApiError("network error — is the Conductor server reachable?", 0);
  }

  if (!res.ok) {
    // The gateway returns {"error":{"message","type"}} on failure; surface the
    // message when present, otherwise fall back to the status line.
    let message = `${res.status} ${res.statusText}`;
    try {
      const body = await res.json();
      if (body?.error?.message) {
        message = body.error.message;
      }
    } catch {
      /* non-JSON body — keep the status line */
    }
    throw new ApiError(message, res.status);
  }
  return (await res.json()) as T;
}

export function fetchTraces(limit: number): Promise<{ data: Trace[] | null }> {
  return apiGet(`/v1/traces?limit=${limit}`);
}

export function fetchTrace(id: string): Promise<Trace> {
  return apiGet(`/v1/traces/${encodeURIComponent(id)}`);
}

async function apiPost<T>(path: string, body: unknown): Promise<T> {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  const key = getApiKey();
  if (key) {
    headers.Authorization = `Bearer ${key}`;
  }

  let res: Response;
  try {
    res = await fetch(path, { method: "POST", headers, body: JSON.stringify(body) });
  } catch {
    throw new ApiError("network error — is the Conductor server reachable?", 0);
  }

  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`;
    try {
      const b = await res.json();
      if (b?.error?.message) message = b.error.message;
    } catch {
      /* non-JSON body — keep the status line */
    }
    throw new ApiError(message, res.status);
  }
  return (await res.json()) as T;
}

export function fetchConfig(): Promise<ConfigSummary> {
  return apiGet("/v1/config");
}

export function fetchWorkflows(): Promise<{ data: Workflow[] | null }> {
  return apiGet("/v1/workflows");
}

export function runWorkflow(name: string, inputs: Record<string, string>): Promise<WorkflowRun> {
  return apiPost(`/v1/workflows/${encodeURIComponent(name)}/run`, { inputs });
}

export function fetchRuns(limit: number): Promise<{ data: WorkflowRun[] | null }> {
  return apiGet(`/v1/workflow-runs?limit=${limit}`);
}

export function fetchRun(id: string): Promise<WorkflowRun> {
  return apiGet(`/v1/workflow-runs/${encodeURIComponent(id)}`);
}

// Wire types mirroring Conductor's gateway JSON. Field names match the Go
// `json:` tags exactly (see core/ports/trace.go and api/http ConfigSummary), so
// responses deserialize without transformation.

export type AttemptStatus = "success" | "error" | "timeout";

export interface Usage {
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
}

export interface Attempt {
  index: number;
  provider: string;
  model: string;
  status: AttemptStatus;
  error?: string;
  latency_ms: number;
  usage: Usage;
  cost_usd: number;
  started: number;
}

export interface Trace {
  id: string;
  created: number;
  request_model: string;
  message_count: number;
  stream: boolean;
  attempts: Attempt[] | null;
  final_provider?: string;
  final_status: AttemptStatus;
  error?: string;
  usage: Usage;
  cost_usd: number;
  latency_ms: number;
}

export interface ModuleSummary {
  name: string;
  use: string;
}

export interface StoreSummary {
  configured: boolean;
  use?: string;
}

export interface ConfigSummary {
  server_address: string;
  request_timeout_seconds: number;
  providers: ModuleSummary[];
  router: ModuleSummary;
  trace_store: StoreSummary;
  prompt_store: StoreSummary;
  note: string;
}

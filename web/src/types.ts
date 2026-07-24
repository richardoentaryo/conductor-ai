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

// --- Workflows (Phase 2) --------------------------------------------------
// Mirror core/ports/workflow.go json tags.

export interface WorkflowNode {
  id: string;
  type?: string;
  prompt: string;
  system?: string;
  model: string;
  depends_on?: string[];
  retries?: number;
  timeout_seconds?: number;
}

export interface Workflow {
  name: string;
  description?: string;
  inputs?: string[];
  nodes: WorkflowNode[];
}

export type RunStatus = "success" | "failed";

export interface NodeResult {
  node_id: string;
  status: AttemptStatus;
  output?: string;
  provider?: string;
  trace_id?: string;
  attempts: number;
  usage: Usage;
  cost_usd: number;
  latency_ms: number;
  error?: string;
}

export interface WorkflowRun {
  id: string;
  workflow: string;
  created: number;
  status: RunStatus;
  trigger?: string;
  nodes: NodeResult[] | null;
  usage: Usage;
  cost_usd: number;
  latency_ms: number;
  error?: string;
}

export interface ScheduleLastRun {
  id: string;
  status: RunStatus;
  created: number;
}

export interface Schedule {
  name: string;
  workflow: string;
  cron: string;
  next: number;
  last_run?: ScheduleLastRun;
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

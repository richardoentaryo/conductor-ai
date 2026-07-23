// WorkflowsView drives the Phase 2 workflow engine from the control plane: it
// lists the workflows loaded on the server, lets an operator supply inputs and
// run one, and shows run history with a per-node breakdown. All aggregation is
// trivial (the backend returns fully-formed WorkflowRun records), so this view
// is mostly form + tables over the /v1/workflows and /v1/workflow-runs APIs.

import { useEffect, useMemo, useState } from "react";
import { fetchWorkflows, runWorkflow, fetchRuns, ApiError } from "../api";
import type { Workflow, WorkflowRun, NodeResult, AttemptStatus } from "../types";
import { usd, ms, timeAgo } from "../format";
import { Card, StatusBadge, ErrorBanner, Spinner, Empty } from "../components/ui";

// runBadgeStatus maps a run's success/failed status onto the AttemptStatus the
// shared StatusBadge understands, so run and node states share one visual language.
function runBadgeStatus(status: string): AttemptStatus {
  return status === "success" ? "success" : "error";
}

export default function WorkflowsView() {
  const [workflows, setWorkflows] = useState<Workflow[] | null>(null);
  const [runs, setRuns] = useState<WorkflowRun[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notConfigured, setNotConfigured] = useState(false);
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState<string | null>(null);
  const [openRun, setOpenRun] = useState<string | null>(null);

  async function loadWorkflows() {
    try {
      const res = await fetchWorkflows();
      const list = res.data ?? [];
      setWorkflows(list);
      setSelected((cur) => cur ?? list[0]?.name ?? null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  async function loadRuns() {
    try {
      const res = await fetchRuns(50);
      setRuns(res.data ?? []);
      setNotConfigured(false);
    } catch (e) {
      // 501 = server has no run store; history is unavailable but running still works.
      if (e instanceof ApiError && e.status === 501) {
        setNotConfigured(true);
        setRuns([]);
      } else {
        setError(e instanceof Error ? e.message : String(e));
      }
    }
  }

  useEffect(() => {
    setLoading(true);
    void Promise.all([loadWorkflows(), loadRuns()]).finally(() => setLoading(false));
  }, []);

  const selectedWf = useMemo(
    () => workflows?.find((w) => w.name === selected) ?? null,
    [workflows, selected],
  );

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold text-slate-100">Workflows</h1>
        <p className="text-sm text-slate-500">
          Run multi-step DAGs; each LLM node inherits routing, fallback, tracing and cost.
        </p>
      </div>

      {error && <ErrorBanner message={error} />}

      {loading ? (
        <Spinner />
      ) : !workflows || workflows.length === 0 ? (
        <Empty>
          No workflows loaded. Set <code className="text-slate-300">workflows.dir</code> in
          config.yaml to a directory of <code className="text-slate-300">*.yaml</code> definitions
          and restart.
        </Empty>
      ) : (
        <div className="grid gap-4 md:grid-cols-[16rem_1fr]">
          {/* Workflow picker */}
          <Card title="Definitions">
            <div className="space-y-1">
              {workflows.map((w) => (
                <button
                  key={w.name}
                  onClick={() => setSelected(w.name)}
                  className={`block w-full rounded-md px-3 py-2 text-left text-sm ${
                    selected === w.name
                      ? "bg-slate-800 text-slate-100"
                      : "text-slate-400 hover:bg-slate-800/50 hover:text-slate-200"
                  }`}
                >
                  <div className="font-medium">{w.name}</div>
                  <div className="truncate text-xs text-slate-500">
                    {w.nodes.length} node{w.nodes.length === 1 ? "" : "s"}
                  </div>
                </button>
              ))}
            </div>
          </Card>

          {selectedWf && <RunPanel workflow={selectedWf} onRan={() => void loadRuns()} />}
        </div>
      )}

      {notConfigured && (
        <ErrorBanner message="No run store configured (GET /v1/workflow-runs returned 501). Configure a trace_store module (e.g. memory.sqlite) to record run history." />
      )}

      <Card
        title="Run history"
        right={
          <button
            onClick={() => void loadRuns()}
            className="rounded-md border border-slate-700 bg-slate-800 px-3 py-1 text-sm text-slate-200 hover:bg-slate-700"
          >
            Refresh
          </button>
        }
      >
        {runs && runs.length > 0 ? (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead className="text-xs uppercase tracking-wide text-slate-500">
                <tr>
                  <th className="py-2 pr-4 font-medium">Time</th>
                  <th className="py-2 pr-4 font-medium">Workflow</th>
                  <th className="py-2 pr-4 font-medium">Status</th>
                  <th className="py-2 pr-4 font-medium">Nodes</th>
                  <th className="py-2 pr-4 text-right font-medium">Latency</th>
                  <th className="py-2 pr-4 text-right font-medium">Cost</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-800">
                {runs.map((r) => (
                  <RunRow
                    key={r.id}
                    run={r}
                    open={openRun === r.id}
                    onToggle={() => setOpenRun(openRun === r.id ? null : r.id)}
                  />
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <Empty>No runs recorded yet. Run a workflow above.</Empty>
        )}
      </Card>
    </div>
  );
}

// RunPanel renders the inputs form for one workflow and its most recent run.
function RunPanel({ workflow, onRan }: { workflow: Workflow; onRan: () => void }) {
  const inputNames = workflow.inputs ?? [];
  const [values, setValues] = useState<Record<string, string>>({});
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<WorkflowRun | null>(null);
  const [err, setErr] = useState<string | null>(null);

  // Reset transient state when the operator switches workflows.
  useEffect(() => {
    setValues({});
    setResult(null);
    setErr(null);
  }, [workflow.name]);

  async function run() {
    setRunning(true);
    setErr(null);
    try {
      setResult(await runWorkflow(workflow.name, values));
      onRan();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setRunning(false);
    }
  }

  return (
    <Card title={workflow.name}>
      <div className="space-y-4">
        {workflow.description && <p className="text-sm text-slate-400">{workflow.description}</p>}

        {inputNames.length > 0 ? (
          <div className="space-y-3">
            {inputNames.map((name) => (
              <div key={name}>
                <label className="text-xs font-medium uppercase tracking-wide text-slate-500">
                  {name}
                </label>
                <textarea
                  rows={2}
                  value={values[name] ?? ""}
                  onChange={(e) => setValues((v) => ({ ...v, [name]: e.target.value }))}
                  className="mt-1 w-full rounded-md border border-slate-700 bg-slate-950 px-2 py-1 text-sm text-slate-200 placeholder:text-slate-600"
                  placeholder={`{{ inputs.${name} }}`}
                />
              </div>
            ))}
          </div>
        ) : (
          <p className="text-sm text-slate-500">This workflow declares no inputs.</p>
        )}

        <button
          onClick={() => void run()}
          disabled={running}
          className="rounded-md bg-indigo-500 px-4 py-1.5 text-sm font-medium text-white hover:bg-indigo-400 disabled:opacity-50"
        >
          {running ? "Running…" : "Run workflow"}
        </button>

        {err && <ErrorBanner message={err} />}

        {result && (
          <div className="space-y-2 border-t border-slate-800 pt-4">
            <div className="flex items-center gap-3 text-sm">
              <StatusBadge status={runBadgeStatus(result.status)} />
              <span className="text-slate-400">
                {ms(result.latency_ms)} · {usd(result.cost_usd)} · {result.usage.total_tokens} tok
              </span>
            </div>
            {result.error && <ErrorBanner message={result.error} />}
            <NodeTable nodes={result.nodes} />
          </div>
        )}
      </div>
    </Card>
  );
}

// RunRow is a history table row that expands to show its node breakdown.
function RunRow({
  run,
  open,
  onToggle,
}: {
  run: WorkflowRun;
  open: boolean;
  onToggle: () => void;
}) {
  return (
    <>
      <tr onClick={onToggle} className="cursor-pointer hover:bg-slate-800/50">
        <td className="py-2 pr-4 text-slate-400">{timeAgo(run.created)}</td>
        <td className="py-2 pr-4 text-slate-300">{run.workflow}</td>
        <td className="py-2 pr-4">
          <StatusBadge status={runBadgeStatus(run.status)} />
        </td>
        <td className="py-2 pr-4 text-slate-400">{run.nodes?.length ?? 0}</td>
        <td className="py-2 pr-4 text-right tabular-nums text-slate-400">{ms(run.latency_ms)}</td>
        <td className="py-2 pr-4 text-right tabular-nums text-slate-400">{usd(run.cost_usd)}</td>
      </tr>
      {open && (
        <tr>
          <td colSpan={6} className="bg-slate-950/40 px-4 py-3">
            {run.error && <div className="mb-2 text-sm text-rose-300">{run.error}</div>}
            <NodeTable nodes={run.nodes} />
          </td>
        </tr>
      )}
    </>
  );
}

// NodeTable lists per-node outcomes shared by the run panel and history rows.
function NodeTable({ nodes }: { nodes: NodeResult[] | null }) {
  if (!nodes || nodes.length === 0) {
    return <Empty>No nodes executed.</Empty>;
  }
  return (
    <div className="space-y-3">
      {nodes.map((n) => (
        <div key={n.node_id} className="rounded-lg border border-slate-800 bg-slate-900/50 p-3">
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <span className="font-medium text-slate-200">{n.node_id}</span>
            <StatusBadge status={n.status} />
            {n.provider && <span className="text-xs text-slate-500">{n.provider}</span>}
            <span className="ml-auto text-xs tabular-nums text-slate-500">
              {ms(n.latency_ms)} · {usd(n.cost_usd)}
              {n.attempts > 1 ? ` · ${n.attempts} attempts` : ""}
            </span>
          </div>
          {n.error && <div className="mt-2 text-sm text-rose-300">{n.error}</div>}
          {n.output && (
            <pre className="mt-2 max-h-48 overflow-auto whitespace-pre-wrap rounded bg-slate-950 p-2 text-xs text-slate-300">
              {n.output}
            </pre>
          )}
        </div>
      ))}
    </div>
  );
}

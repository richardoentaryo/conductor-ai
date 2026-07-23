// TraceDetailView renders the full fallback story of one request: each attempt
// in order with its provider, status, latency, cost and error, then the final
// outcome and aggregate totals. This is the failover narrative the whole trace
// store exists to make legible.

import { useEffect, useState } from "react";
import { fetchTrace } from "../api";
import type { Trace } from "../types";
import { usd, ms, timeAgo } from "../format";
import { Card, StatTile, StatusBadge, ErrorBanner, Spinner } from "../components/ui";

export default function TraceDetailView({
  id,
  onBack,
}: {
  id: string;
  onBack: () => void;
}) {
  const [trace, setTrace] = useState<Trace | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let live = true;
    setLoading(true);
    fetchTrace(id)
      .then((t) => live && setTrace(t))
      .catch((e) => live && setError(e instanceof Error ? e.message : String(e)))
      .finally(() => live && setLoading(false));
    return () => {
      live = false;
    };
  }, [id]);

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <button
          onClick={onBack}
          className="rounded-md border border-slate-700 bg-slate-800 px-3 py-1 text-sm text-slate-200 hover:bg-slate-700"
        >
          ← Back
        </button>
        <h1 className="text-xl font-semibold text-slate-100">Trace</h1>
        <code className="rounded bg-slate-800 px-2 py-0.5 text-xs text-slate-400">{id}</code>
      </div>

      {loading && <Spinner />}
      {error && <ErrorBanner message={error} />}

      {trace && (
        <>
          <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
            <StatTile label="Final status" value={<StatusBadge status={trace.final_status} />} />
            <StatTile label="Final provider" value={trace.final_provider || "—"} />
            <StatTile label="Total latency" value={ms(trace.latency_ms)} />
            <StatTile label="Total cost" value={usd(trace.cost_usd)} />
          </div>

          <Card title="Request">
            <dl className="grid grid-cols-2 gap-y-3 text-sm md:grid-cols-4">
              <Field label="Model" value={trace.request_model} />
              <Field label="Messages" value={String(trace.message_count)} />
              <Field label="Streamed" value={trace.stream ? "yes" : "no"} />
              <Field label="Created" value={timeAgo(trace.created)} />
              <Field label="Prompt tokens" value={String(trace.usage.prompt_tokens)} />
              <Field label="Completion tokens" value={String(trace.usage.completion_tokens)} />
              <Field label="Total tokens" value={String(trace.usage.total_tokens)} />
            </dl>
          </Card>

          {trace.error && <ErrorBanner message={`Request error: ${trace.error}`} />}

          <Card title={`Fallback chain (${trace.attempts?.length ?? 0} attempt${(trace.attempts?.length ?? 0) === 1 ? "" : "s"})`}>
            <ol className="space-y-3">
              {(trace.attempts ?? []).map((a) => (
                <li
                  key={a.index}
                  className="rounded-lg border border-slate-800 bg-slate-900/40 p-3"
                >
                  <div className="flex flex-wrap items-center gap-3">
                    <span className="flex h-6 w-6 items-center justify-center rounded-full bg-slate-800 text-xs text-slate-400">
                      {a.index + 1}
                    </span>
                    <span className="font-medium text-slate-200">{a.provider}</span>
                    <span className="text-xs text-slate-500">{a.model}</span>
                    <StatusBadge status={a.status} />
                    <span className="ml-auto flex gap-4 text-xs tabular-nums text-slate-400">
                      <span>{ms(a.latency_ms)}</span>
                      <span>{usd(a.cost_usd)}</span>
                      <span>{a.usage.total_tokens} tok</span>
                    </span>
                  </div>
                  {a.error && (
                    <div className="mt-2 rounded border border-rose-500/20 bg-rose-500/5 px-2 py-1 text-xs text-rose-300">
                      {a.error}
                    </div>
                  )}
                </li>
              ))}
            </ol>
          </Card>
        </>
      )}
    </div>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs uppercase tracking-wide text-slate-500">{label}</dt>
      <dd className="mt-0.5 text-slate-200">{value}</dd>
    </div>
  );
}

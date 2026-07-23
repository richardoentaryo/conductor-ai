// TracesView is the observability landing page: a page of recent traces plus
// summary tiles (cost, volume, error rate, cost-by-provider) computed entirely
// client-side from the fetched page — the backend exposes no aggregate endpoint,
// so aggregation lives here over whatever slice the operator chose to load.

import { useEffect, useMemo, useState } from "react";
import { fetchTraces, ApiError } from "../api";
import type { Trace } from "../types";
import { usd, ms, timeAgo } from "../format";
import { Card, StatTile, StatusBadge, ErrorBanner, Spinner, Empty } from "../components/ui";

function summarize(traces: Trace[]) {
  let totalCost = 0;
  let errors = 0;
  const byProvider = new Map<string, number>();
  for (const t of traces) {
    totalCost += t.cost_usd;
    if (t.final_status !== "success") errors += 1;
    const p = t.final_provider || "(none)";
    byProvider.set(p, (byProvider.get(p) ?? 0) + t.cost_usd);
  }
  const errorRate = traces.length ? (errors / traces.length) * 100 : 0;
  const providers = [...byProvider.entries()].sort((a, b) => b[1] - a[1]);
  return { totalCost, errors, errorRate, providers };
}

export default function TracesView({
  onOpen,
}: {
  onOpen: (id: string) => void;
}) {
  const [limit, setLimit] = useState(50);
  const [traces, setTraces] = useState<Trace[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [notConfigured, setNotConfigured] = useState(false);

  async function load() {
    setLoading(true);
    setError(null);
    setNotConfigured(false);
    try {
      const res = await fetchTraces(limit);
      setTraces(res.data ?? []);
    } catch (e) {
      if (e instanceof ApiError && e.status === 501) {
        setNotConfigured(true);
        setTraces([]);
      } else {
        setError(e instanceof Error ? e.message : String(e));
      }
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [limit]);

  const stats = useMemo(() => summarize(traces ?? []), [traces]);

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold text-slate-100">Traces</h1>
          <p className="text-sm text-slate-500">
            Request-level observability: fallback chains, latency and cost.
          </p>
        </div>
        <div className="flex items-center gap-2 text-sm">
          <label className="text-slate-400">Load</label>
          <select
            value={limit}
            onChange={(e) => setLimit(Number(e.target.value))}
            className="rounded-md border border-slate-700 bg-slate-900 px-2 py-1 text-slate-200"
          >
            {[20, 50, 100, 200, 500].map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
          <button
            onClick={() => void load()}
            className="rounded-md border border-slate-700 bg-slate-800 px-3 py-1 text-slate-200 hover:bg-slate-700"
          >
            Refresh
          </button>
        </div>
      </div>

      {notConfigured && (
        <ErrorBanner message="No trace store is configured on this server (GET /v1/traces returned 501). Configure a trace_store module to record observability data." />
      )}
      {error && <ErrorBanner message={error} />}

      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        <StatTile label="Requests" value={traces?.length ?? "—"} hint="in loaded page" />
        <StatTile label="Total cost" value={usd(stats.totalCost)} hint="loaded page" />
        <StatTile
          label="Error rate"
          value={`${stats.errorRate.toFixed(1)}%`}
          hint={`${stats.errors} failed`}
        />
        <StatTile label="Providers" value={stats.providers.length} hint="distinct finals" />
      </div>

      {stats.providers.length > 0 && (
        <Card title="Cost by provider">
          <div className="space-y-2">
            {stats.providers.map(([name, cost]) => {
              const pct = stats.totalCost > 0 ? (cost / stats.totalCost) * 100 : 0;
              return (
                <div key={name} className="flex items-center gap-3 text-sm">
                  <div className="w-32 shrink-0 truncate text-slate-300">{name}</div>
                  <div className="h-2 flex-1 overflow-hidden rounded-full bg-slate-800">
                    <div
                      className="h-full rounded-full bg-indigo-500"
                      style={{ width: `${pct}%` }}
                    />
                  </div>
                  <div className="w-24 shrink-0 text-right tabular-nums text-slate-400">
                    {usd(cost)}
                  </div>
                </div>
              );
            })}
          </div>
        </Card>
      )}

      <Card title="Recent requests">
        {loading ? (
          <Spinner />
        ) : traces && traces.length > 0 ? (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead className="text-xs uppercase tracking-wide text-slate-500">
                <tr>
                  <th className="py-2 pr-4 font-medium">Time</th>
                  <th className="py-2 pr-4 font-medium">Model</th>
                  <th className="py-2 pr-4 font-medium">Final provider</th>
                  <th className="py-2 pr-4 font-medium">Status</th>
                  <th className="py-2 pr-4 font-medium">Attempts</th>
                  <th className="py-2 pr-4 text-right font-medium">Latency</th>
                  <th className="py-2 pr-4 text-right font-medium">Cost</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-800">
                {traces.map((t) => (
                  <tr
                    key={t.id}
                    onClick={() => onOpen(t.id)}
                    className="cursor-pointer hover:bg-slate-800/50"
                  >
                    <td className="py-2 pr-4 text-slate-400">{timeAgo(t.created)}</td>
                    <td className="py-2 pr-4 text-slate-300">{t.request_model}</td>
                    <td className="py-2 pr-4 text-slate-300">{t.final_provider || "—"}</td>
                    <td className="py-2 pr-4">
                      <StatusBadge status={t.final_status} />
                    </td>
                    <td className="py-2 pr-4 text-slate-400">{t.attempts?.length ?? 0}</td>
                    <td className="py-2 pr-4 text-right tabular-nums text-slate-400">
                      {ms(t.latency_ms)}
                    </td>
                    <td className="py-2 pr-4 text-right tabular-nums text-slate-400">
                      {usd(t.cost_usd)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <Empty>No traces recorded yet. Send a request to /v1/chat/completions.</Empty>
        )}
      </Card>
    </div>
  );
}

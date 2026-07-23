// ConfigView renders the active runtime configuration read-only. The backend
// serves a redacted summary with no module Settings (secrets never cross the
// wire), and offers no write path — so this view shows a prominent notice that
// changes require editing config.yaml and restarting.

import { useEffect, useState } from "react";
import { fetchConfig } from "../api";
import type { ConfigSummary } from "../types";
import { Card, ErrorBanner, Spinner } from "../components/ui";

export default function ConfigView() {
  const [cfg, setCfg] = useState<ConfigSummary | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let live = true;
    fetchConfig()
      .then((c) => live && setCfg(c))
      .catch((e) => live && setError(e instanceof Error ? e.message : String(e)))
      .finally(() => live && setLoading(false));
    return () => {
      live = false;
    };
  }, []);

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold text-slate-100">Configuration</h1>
        <p className="text-sm text-slate-500">Active runtime configuration (redacted).</p>
      </div>

      {loading && <Spinner />}
      {error && <ErrorBanner message={error} />}

      {cfg && (
        <>
          <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-200">
            {cfg.note || "Read-only. Edit config.yaml and restart to change."}
          </div>

          <Card title="Server">
            <dl className="grid grid-cols-2 gap-y-3 text-sm">
              <Field label="Address" value={cfg.server_address} />
              <Field label="Request timeout" value={`${cfg.request_timeout_seconds}s`} />
            </dl>
          </Card>

          <Card title={`Providers (${cfg.providers.length})`}>
            <div className="overflow-x-auto">
              <table className="w-full text-left text-sm">
                <thead className="text-xs uppercase tracking-wide text-slate-500">
                  <tr>
                    <th className="py-2 pr-4 font-medium">Name</th>
                    <th className="py-2 pr-4 font-medium">Module</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-800">
                  {cfg.providers.map((p) => (
                    <tr key={p.name}>
                      <td className="py-2 pr-4 text-slate-200">{p.name}</td>
                      <td className="py-2 pr-4">
                        <code className="text-slate-400">{p.use}</code>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Card>

          <div className="grid gap-4 md:grid-cols-3">
            <Card title="Router">
              <ModuleLine name={cfg.router.name} use={cfg.router.use} />
            </Card>
            <Card title="Trace store">
              <StoreLine configured={cfg.trace_store.configured} use={cfg.trace_store.use} />
            </Card>
            <Card title="Prompt store">
              <StoreLine configured={cfg.prompt_store.configured} use={cfg.prompt_store.use} />
            </Card>
          </div>
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

function ModuleLine({ name, use }: { name: string; use: string }) {
  return (
    <div className="text-sm">
      <div className="text-slate-200">{name || "—"}</div>
      <code className="text-xs text-slate-500">{use}</code>
    </div>
  );
}

function StoreLine({ configured, use }: { configured: boolean; use?: string }) {
  if (!configured) {
    return <div className="text-sm text-slate-500">Not configured</div>;
  }
  return (
    <div className="text-sm">
      <span className="inline-flex items-center rounded-full bg-emerald-500/15 px-2 py-0.5 text-xs font-medium text-emerald-300 ring-1 ring-inset ring-emerald-500/30">
        configured
      </span>
      <code className="mt-1 block text-xs text-slate-500">{use}</code>
    </div>
  );
}

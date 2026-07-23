// App is the dashboard shell: a fixed sidebar for navigation, an API-key control
// (persisted to localStorage and sent as a Bearer token by api.ts), and a simple
// state-driven view switch. State-based routing — rather than a client router —
// keeps the SPA a single "/" document, so the Go file server needs no deep-link
// fallback.

import { useState } from "react";
import { getApiKey, setApiKey } from "./api";
import TracesView from "./views/TracesView";
import TraceDetailView from "./views/TraceDetailView";
import ConfigView from "./views/ConfigView";
import WorkflowsView from "./views/WorkflowsView";

type Nav = "traces" | "config" | "workflows";

const NAV: { id: Nav; label: string; icon: string }[] = [
  { id: "traces", label: "Traces", icon: "◧" },
  { id: "config", label: "Config", icon: "⚙" },
  { id: "workflows", label: "Workflows", icon: "⤳" },
];

export default function App() {
  const [nav, setNav] = useState<Nav>("traces");
  const [openTrace, setOpenTrace] = useState<string | null>(null);

  function go(target: Nav) {
    setOpenTrace(null);
    setNav(target);
  }

  return (
    <div className="min-h-screen bg-slate-950 text-slate-200">
      <div className="mx-auto flex max-w-7xl">
        <aside className="sticky top-0 hidden h-screen w-56 shrink-0 flex-col border-r border-slate-800 p-4 md:flex">
          <div className="mb-6 flex items-center gap-2 px-2">
            <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-indigo-500 text-sm font-bold text-white">
              C
            </span>
            <div>
              <div className="text-sm font-semibold text-slate-100">Conductor</div>
              <div className="text-xs text-slate-500">Control Plane</div>
            </div>
          </div>
          <nav className="space-y-1">
            {NAV.map((item) => (
              <button
                key={item.id}
                onClick={() => go(item.id)}
                className={`flex w-full items-center gap-3 rounded-lg px-3 py-2 text-sm ${
                  nav === item.id && !openTrace
                    ? "bg-slate-800 text-slate-100"
                    : "text-slate-400 hover:bg-slate-800/50 hover:text-slate-200"
                }`}
              >
                <span className="w-4 text-center text-slate-500">{item.icon}</span>
                {item.label}
              </button>
            ))}
          </nav>
          <div className="mt-auto pt-4">
            <ApiKeyControl />
          </div>
        </aside>

        <main className="min-w-0 flex-1 p-6">
          {/* Mobile nav */}
          <div className="mb-4 flex gap-2 md:hidden">
            {NAV.map((item) => (
              <button
                key={item.id}
                onClick={() => go(item.id)}
                className={`rounded-md px-3 py-1 text-sm ${
                  nav === item.id ? "bg-slate-800 text-slate-100" : "text-slate-400"
                }`}
              >
                {item.label}
              </button>
            ))}
          </div>

          {openTrace ? (
            <TraceDetailView id={openTrace} onBack={() => setOpenTrace(null)} />
          ) : nav === "traces" ? (
            <TracesView onOpen={(id) => setOpenTrace(id)} />
          ) : nav === "config" ? (
            <ConfigView />
          ) : (
            <WorkflowsView />
          )}
        </main>
      </div>
    </div>
  );
}

function ApiKeyControl() {
  const [key, setKey] = useState(getApiKey());
  const [saved, setSaved] = useState(false);

  function save() {
    setApiKey(key.trim());
    setSaved(true);
    setTimeout(() => setSaved(false), 1500);
  }

  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900/50 p-3">
      <label className="text-xs font-medium uppercase tracking-wide text-slate-500">
        API key
      </label>
      <input
        type="password"
        value={key}
        onChange={(e) => setKey(e.target.value)}
        placeholder="leave empty if keyless"
        className="mt-1 w-full rounded-md border border-slate-700 bg-slate-950 px-2 py-1 text-sm text-slate-200 placeholder:text-slate-600"
      />
      <button
        onClick={save}
        className="mt-2 w-full rounded-md bg-indigo-500 px-2 py-1 text-sm font-medium text-white hover:bg-indigo-400"
      >
        {saved ? "Saved" : "Save"}
      </button>
      <p className="mt-1 text-[11px] leading-tight text-slate-600">
        Sent as a Bearer token on /v1 requests. Stored in this browser only.
      </p>
    </div>
  );
}

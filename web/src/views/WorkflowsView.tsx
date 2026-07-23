// WorkflowsView is a deliberate placeholder. The Phase 2 workflow/DAG builder is
// out of scope for this stage; the nav item exists only to signal it is coming,
// with no backend behind it.

import { Empty } from "../components/ui";

export default function WorkflowsView() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold text-slate-100">Workflows</h1>
        <p className="text-sm text-slate-500">Visual DAG builder for multi-step orchestration.</p>
      </div>
      <Empty>
        <div className="space-y-2">
          <div className="text-base font-medium text-slate-300">Coming soon</div>
          <p className="mx-auto max-w-md">
            A workflow / DAG builder is planned for a future release. There is nothing to
            configure here yet.
          </p>
        </div>
      </Empty>
    </div>
  );
}

import type { Evidence, Job, Overview, Page, Repository, Run, Worker, Workflow, WorkflowDetail } from './types'

type WorkflowActionWire = {
  id: string
  kind: string
  role: string
  mode: string
  target_state: string
  revision: string
  budget_kind?: string
  status: string
  job_id?: string
  decision?: string
  created_at: string
  completed_at?: string
}

type WorkflowHistoryWire = {
  id: number
  from_state: string
  to_state: string
  actor_role: string
  reason?: string
  created_at: string
}

type WorkflowDetailWire = {
  workflow: Workflow
  actions: WorkflowActionWire[] | null
  history: WorkflowHistoryWire[] | null
}

export class OperatorApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message)
  }
}

export class OperatorApi {
  constructor(private readonly token: string) {}

  private async get<T>(path: string): Promise<T> {
    const response = await fetch(path, {
      method: 'GET',
      cache: 'no-store',
      headers: {
        Authorization: `Bearer ${this.token}`,
        Accept: 'application/json',
      },
    })
    if (!response.ok) {
      let message = `request failed (${response.status})`
      try {
        const body = (await response.json()) as { error?: string }
        if (body.error) message = body.error
      } catch {
        // Keep the generic status message when the response is not JSON.
      }
      throw new OperatorApiError(response.status, message)
    }
    return (await response.json()) as T
  }

  overview(): Promise<Overview> {
    return this.get('/api/v1/overview')
  }

  repositories(): Promise<{ items: Repository[] | null }> {
    return this.get('/api/v1/repositories')
  }

  jobs(limit = 100): Promise<Page<Job>> {
    return this.get(`/api/v1/jobs?limit=${limit}`)
  }

  workflows(limit = 100): Promise<Page<Workflow>> {
    return this.get(`/api/v1/workflows?limit=${limit}`)
  }

  async workflow(id: string): Promise<WorkflowDetail> {
    const wire = await this.get<WorkflowDetailWire>(`/api/v1/workflows/${encodeURIComponent(id)}`)
    return {
      workflow: wire.workflow,
      actions: wire.actions?.map((item) => ({
        ID: item.id,
        Kind: item.kind,
        Role: item.role,
        Mode: item.mode,
        TargetState: item.target_state,
        Revision: item.revision,
        BudgetKind: item.budget_kind ?? '',
        Status: item.status,
        JobID: item.job_id ?? '',
        Decision: item.decision ?? '',
        CreatedAt: item.created_at,
        CompletedAt: item.completed_at,
      })) ?? null,
      history: wire.history?.map((item) => ({
        ID: item.id,
        FromState: item.from_state,
        ToState: item.to_state,
        ActorRole: item.actor_role,
        Reason: item.reason ?? '',
        CreatedAt: item.created_at,
      })) ?? null,
    }
  }

  runs(limit = 100): Promise<Page<Run>> {
    return this.get(`/api/v1/runs?limit=${limit}`)
  }

  evidence(runID: string): Promise<{ items: Evidence[] | null }> {
    return this.get(`/api/v1/runs/${encodeURIComponent(runID)}/evidence`)
  }

  workers(): Promise<{ items: Worker[] | null }> {
    return this.get('/api/v1/workers')
  }
}

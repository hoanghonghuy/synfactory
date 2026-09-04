import type {
  Evidence,
  Job,
  Overview,
  Page,
  Repository,
  RepositoryConfig,
  RepositoryConfigAudit,
  RepositoryConfigInput,
  Run,
  Worker,
  Workflow,
  WorkflowDetail,
} from './types'

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

  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const headers = new Headers(init.headers)
    headers.set('Authorization', `Bearer ${this.token}`)
    headers.set('Accept', 'application/json')
    if (init.body !== undefined) headers.set('Content-Type', 'application/json')

    const response = await fetch(path, {
      ...init,
      cache: 'no-store',
      headers,
    })
    if (!response.ok) {
      let message = `request failed (${response.status})`
      try {
        const body = (await response.json()) as { error?: string; message?: string }
        if (body.message) message = body.message
        else if (body.error) message = body.error
      } catch {
        // Keep the generic status message when the response is not JSON.
      }
      throw new OperatorApiError(response.status, message)
    }
    return (await response.json()) as T
  }

  private get<T>(path: string): Promise<T> {
    return this.request(path, { method: 'GET' })
  }

  overview(): Promise<Overview> {
    return this.get('/api/v1/overview')
  }

  repositories(): Promise<{ items: Repository[] | null }> {
    return this.get('/api/v1/repositories')
  }

  repositoryConfigs(): Promise<{ items: RepositoryConfig[] | null }> {
    return this.get('/api/v1/repository-config')
  }

  registerRepository(input: RepositoryConfigInput & { full_name: string }): Promise<RepositoryConfig> {
    return this.request('/api/v1/repository-config', {
      method: 'POST',
      body: JSON.stringify(input),
    })
  }

  updateRepository(id: string, input: RepositoryConfigInput): Promise<RepositoryConfig> {
    return this.request(`/api/v1/repository-config/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    })
  }

  repositoryAudit(id: string): Promise<{ items: RepositoryConfigAudit[] | null }> {
    return this.get(`/api/v1/repository-config/${encodeURIComponent(id)}/audit`)
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

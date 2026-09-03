import type { Evidence, Job, Overview, Page, Repository, Run, Worker, Workflow, WorkflowDetail } from './types'

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

  workflow(id: string): Promise<WorkflowDetail> {
    return this.get(`/api/v1/workflows/${encodeURIComponent(id)}`)
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

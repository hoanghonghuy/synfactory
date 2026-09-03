export type OperationalStats = {
  queued_jobs: number
  active_jobs: number
  stale_job_leases: number
  pending_events: number
  failed_events: number
  blocked_workflows: number
  parked_workflows: number
  live_workers: number
  stale_workers: number
}

export type Repository = {
  id: string
  provider: string
  full_name: string
  default_branch: string
  enabled: boolean
  updated_at: string
}

export type Worker = {
  id: string
  host: string
  capacity: number
  draining: boolean
  last_heartbeat: string
  started_at: string
  healthy: boolean
}

export type Workflow = {
  id: string
  repository_id: string
  kind: string
  subject: string
  revision: string
  state: string
  priority: number
  blocked_reason?: string
  ci_repair_attempts: number
  ci_repair_limit: number
  review_repair_attempts: number
  review_repair_limit: number
  last_dispatched_at?: string
  created_at: string
  updated_at: string
}

export type WorkflowAction = {
  ID: string
  Kind: string
  Role: string
  Mode: string
  TargetState: string
  Revision: string
  BudgetKind: string
  Status: string
  JobID: string
  Decision: string
  CreatedAt: string
  CompletedAt?: string
}

export type WorkflowHistory = {
  ID: number
  FromState: string
  ToState: string
  ActorRole: string
  Reason: string
  CreatedAt: string
}

export type WorkflowDetail = {
  workflow: Workflow
  actions: WorkflowAction[] | null
  history: WorkflowHistory[] | null
}

export type Job = {
  id: string
  repository_id: string
  kind: string
  role: string
  subject: string
  revision: string
  priority: number
  status: string
  attempt: number
  max_attempts: number
  available_at: string
  lease_owner?: string
  lease_until?: string
  last_error?: string
  created_at: string
  updated_at: string
}

export type Run = {
  id: string
  job_id: string
  attempt: number
  sequence: number
  runtime: string
  model?: string
  status: string
  started_at: string
  finished_at?: string
  exit_code?: number
  summary?: string
}

export type Evidence = {
  id: number
  run_id: string
  kind: string
  name: string
  uri?: string
  sha256?: string
  created_at: string
}

export type Overview = {
  generated_at: string
  stats: OperationalStats
  workers: Worker[] | null
  attention_workflows: Workflow[] | null
}

export type Page<T> = {
  items: T[] | null
  limit: number
  offset: number
}

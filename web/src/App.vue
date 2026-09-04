<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { OperatorApi, OperatorApiError } from './api'
import type {
  Evidence,
  Job,
  Overview,
  Repository,
  RepositoryConfig,
  RepositoryConfigAudit,
  Run,
  Worker,
  Workflow,
  WorkflowDetail,
} from './types'

type View = 'overview' | 'workflows' | 'jobs' | 'runs' | 'repositories' | 'workers'

const storedToken = sessionStorage.getItem('synfactory.operator.token') ?? ''
const token = ref(storedToken)
const tokenInput = ref('')
const view = ref<View>('overview')
const overview = ref<Overview | null>(null)
const repositories = ref<Repository[]>([])
const repositoryConfigs = ref<RepositoryConfig[]>([])
const repositoryAudit = ref<RepositoryConfigAudit[]>([])
const auditedRepository = ref<RepositoryConfig | null>(null)
const repositoryFullName = ref('')
const repositoryDefaultBranch = ref('main')
const repositoryIntegrationBranch = ref('develop')
const repositoryWorkspacePolicy = ref('')
const repositorySaving = ref(false)
const workflows = ref<Workflow[]>([])
const jobs = ref<Job[]>([])
const runs = ref<Run[]>([])
const workers = ref<Worker[]>([])
const selectedWorkflow = ref<WorkflowDetail | null>(null)
const selectedRun = ref<Run | null>(null)
const selectedEvidence = ref<Evidence[]>([])
const loading = ref(false)
const error = ref('')
const lastUpdated = ref<Date | null>(null)

const api = computed(() => (token.value ? new OperatorApi(token.value) : null))
const repositoryNames = computed(() => {
  const names = new Map(repositories.value.map((item) => [item.id, item.full_name]))
  for (const item of repositoryConfigs.value) names.set(item.id, item.full_name)
  return names
})
const attentionCount = computed(() => overview.value?.attention_workflows?.length ?? 0)

async function refresh(): Promise<void> {
  if (!api.value || loading.value) return
  loading.value = true
  error.value = ''
  try {
    const [nextOverview, nextRepositories, nextRepositoryConfigs, nextJobs, nextWorkflows, nextRuns, nextWorkers] = await Promise.all([
      api.value.overview(),
      api.value.repositories(),
      api.value.repositoryConfigs(),
      api.value.jobs(),
      api.value.workflows(),
      api.value.runs(),
      api.value.workers(),
    ])
    overview.value = nextOverview
    repositories.value = nextRepositories.items ?? []
    repositoryConfigs.value = nextRepositoryConfigs.items ?? []
    jobs.value = nextJobs.items ?? []
    workflows.value = nextWorkflows.items ?? []
    runs.value = nextRuns.items ?? []
    workers.value = nextWorkers.items ?? []
    lastUpdated.value = new Date()
  } catch (cause) {
    if (cause instanceof OperatorApiError && cause.status === 401) {
      lock()
      error.value = 'Operator token was rejected.'
    } else {
      error.value = cause instanceof Error ? cause.message : 'Unable to refresh SynFactory state.'
    }
  } finally {
    loading.value = false
  }
}

async function unlock(): Promise<void> {
  const candidate = tokenInput.value.trim()
  if (!candidate) return
  token.value = candidate
  sessionStorage.setItem('synfactory.operator.token', candidate)
  tokenInput.value = ''
  await refresh()
}

function lock(): void {
  token.value = ''
  sessionStorage.removeItem('synfactory.operator.token')
  overview.value = null
  repositories.value = []
  repositoryConfigs.value = []
  repositoryAudit.value = []
  auditedRepository.value = null
  workflows.value = []
  jobs.value = []
  runs.value = []
  workers.value = []
  selectedWorkflow.value = null
  selectedRun.value = null
  selectedEvidence.value = []
}

async function registerRepository(): Promise<void> {
  if (!api.value || repositorySaving.value) return
  const fullName = repositoryFullName.value.trim()
  if (!fullName) return
  repositorySaving.value = true
  error.value = ''
  try {
    await api.value.registerRepository({
      full_name: fullName,
      default_branch: repositoryDefaultBranch.value.trim() || 'main',
      integration_branch: repositoryIntegrationBranch.value.trim() || 'develop',
      workspace_policy: repositoryWorkspacePolicy.value.trim() || undefined,
      enabled: true,
    })
    repositoryFullName.value = ''
    repositoryWorkspacePolicy.value = ''
    await refresh()
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : 'Unable to register repository.'
  } finally {
    repositorySaving.value = false
  }
}

async function toggleRepository(item: RepositoryConfig): Promise<void> {
  if (!api.value || repositorySaving.value) return
  repositorySaving.value = true
  error.value = ''
  try {
    await api.value.updateRepository(item.id, { enabled: !item.enabled })
    if (auditedRepository.value?.id === item.id) auditedRepository.value = null
    await refresh()
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : 'Unable to update repository state.'
  } finally {
    repositorySaving.value = false
  }
}

async function inspectRepositoryAudit(item: RepositoryConfig): Promise<void> {
  if (!api.value) return
  error.value = ''
  try {
    const result = await api.value.repositoryAudit(item.id)
    auditedRepository.value = item
    repositoryAudit.value = result.items ?? []
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : 'Unable to load repository audit.'
  }
}

async function inspectWorkflow(id: string): Promise<void> {
  if (!api.value) return
  error.value = ''
  try {
    selectedWorkflow.value = await api.value.workflow(id)
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : 'Unable to load workflow detail.'
  }
}

async function inspectRun(run: Run): Promise<void> {
  if (!api.value) return
  selectedRun.value = run
  selectedEvidence.value = []
  error.value = ''
  try {
    const result = await api.value.evidence(run.id)
    selectedEvidence.value = result.items ?? []
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : 'Unable to load run evidence.'
  }
}

function closeDetail(): void {
  selectedWorkflow.value = null
  selectedRun.value = null
  selectedEvidence.value = []
}

function repositoryName(id: string): string {
  return repositoryNames.value.get(id) ?? id
}

function formatTime(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function statusClass(status: string): string {
  const normalized = status.toLowerCase()
  if (['succeeded', 'completed', 'passing', 'approved', 'healthy'].includes(normalized)) return 'good'
  if (['failed', 'blocked', 'parked', 'failing', 'cancelled', 'timed_out'].includes(normalized)) return 'bad'
  if (['running', 'leased', 'reviewing', 'verifying', 'implementing', 'merge_gating'].includes(normalized)) return 'active'
  return 'neutral'
}

const timer = window.setInterval(() => {
  if (token.value && document.visibilityState === 'visible') void refresh()
}, 5000)

onBeforeUnmount(() => window.clearInterval(timer))

if (token.value) void refresh()
</script>

<template>
  <main v-if="!token" class="unlock-shell">
    <section class="unlock-card">
      <div class="brand-mark">SF</div>
      <p class="eyebrow">Private operator surface</p>
      <h1>SynFactory Control Center</h1>
      <p class="muted">Enter the operator token configured on the Go control plane. The token is kept only for this browser session.</p>
      <form class="unlock-form" @submit.prevent="unlock">
        <input v-model="tokenInput" type="password" autocomplete="current-password" placeholder="Operator token" autofocus />
        <button type="submit">Unlock</button>
      </form>
      <p v-if="error" class="error-banner">{{ error }}</p>
      <p class="security-note">For production, keep this UI host-local or behind a VPN/SSH tunnel. Public Caddy routes continue to expose only webhook and health endpoints.</p>
    </section>
  </main>

  <div v-else class="app-shell">
    <aside class="sidebar">
      <div class="brand-row">
        <div class="brand-mark small">SF</div>
        <div>
          <strong>SynFactory</strong>
          <span>Control Center</span>
        </div>
      </div>
      <nav>
        <button :class="{ selected: view === 'overview' }" @click="view = 'overview'">Overview <span v-if="attentionCount" class="count">{{ attentionCount }}</span></button>
        <button :class="{ selected: view === 'workflows' }" @click="view = 'workflows'">Workflows</button>
        <button :class="{ selected: view === 'jobs' }" @click="view = 'jobs'">Jobs</button>
        <button :class="{ selected: view === 'runs' }" @click="view = 'runs'">Runs & Evidence</button>
        <button :class="{ selected: view === 'repositories' }" @click="view = 'repositories'">Repositories</button>
        <button :class="{ selected: view === 'workers' }" @click="view = 'workers'">Workers</button>
      </nav>
      <div class="sidebar-footer">
        <span>{{ lastUpdated ? `Updated ${lastUpdated.toLocaleTimeString()}` : 'Not synced' }}</span>
        <button class="text-button" @click="lock">Lock</button>
      </div>
    </aside>

    <section class="content">
      <header class="topbar">
        <div>
          <p class="eyebrow">Autonomous software operations</p>
          <h1>{{ view === 'overview' ? 'Factory overview' : view.replace('_', ' ') }}</h1>
        </div>
        <button class="refresh" :disabled="loading" @click="refresh">{{ loading ? 'Refreshing…' : 'Refresh' }}</button>
      </header>

      <p v-if="error" class="error-banner">{{ error }}</p>

      <template v-if="view === 'overview'">
        <div class="stat-grid">
          <article class="stat-card"><span>Queued</span><strong>{{ overview?.stats.queued_jobs ?? 0 }}</strong><small>ready for workers</small></article>
          <article class="stat-card"><span>Active</span><strong>{{ overview?.stats.active_jobs ?? 0 }}</strong><small>leased or running</small></article>
          <article class="stat-card warning"><span>Blocked</span><strong>{{ overview?.stats.blocked_workflows ?? 0 }}</strong><small>waiting on a material change</small></article>
          <article class="stat-card danger"><span>Parked</span><strong>{{ overview?.stats.parked_workflows ?? 0 }}</strong><small>repair budget exhausted</small></article>
          <article class="stat-card"><span>Workers</span><strong>{{ overview?.stats.live_workers ?? 0 }}</strong><small>{{ overview?.stats.stale_workers ?? 0 }} stale</small></article>
          <article class="stat-card"><span>Events</span><strong>{{ overview?.stats.pending_events ?? 0 }}</strong><small>{{ overview?.stats.failed_events ?? 0 }} failed</small></article>
        </div>

        <section class="panel">
          <div class="panel-heading"><div><p class="eyebrow">Needs attention</p><h2>Blocked and parked workflows</h2></div><span>{{ attentionCount }} items</span></div>
          <div v-if="!attentionCount" class="empty">No blocked or parked workflows.</div>
          <div v-else class="table-wrap">
            <table>
              <thead><tr><th>Repository</th><th>Subject</th><th>State</th><th>Reason</th><th>Repair budget</th><th></th></tr></thead>
              <tbody>
                <tr v-for="item in overview?.attention_workflows ?? []" :key="item.id">
                  <td>{{ repositoryName(item.repository_id) }}</td>
                  <td>#{{ item.subject }}</td>
                  <td><span class="badge" :class="statusClass(item.state)">{{ item.state }}</span></td>
                  <td class="wide">{{ item.blocked_reason || '—' }}</td>
                  <td>CI {{ item.ci_repair_attempts }}/{{ item.ci_repair_limit }} · Review {{ item.review_repair_attempts }}/{{ item.review_repair_limit }}</td>
                  <td><button class="link-button" @click="inspectWorkflow(item.id)">Inspect</button></td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>

        <section class="panel">
          <div class="panel-heading"><div><p class="eyebrow">Capacity</p><h2>Worker health</h2></div><span>{{ workers.length }} registered</span></div>
          <div class="worker-grid">
            <article v-for="worker in workers" :key="worker.id" class="worker-card">
              <div><strong>{{ worker.id }}</strong><span>{{ worker.host }}</span></div>
              <span class="badge" :class="worker.healthy ? 'good' : 'bad'">{{ worker.draining ? 'draining' : worker.healthy ? 'healthy' : 'stale' }}</span>
              <small>Heartbeat {{ formatTime(worker.last_heartbeat) }}</small>
            </article>
          </div>
        </section>
      </template>

      <section v-else-if="view === 'workflows'" class="panel full">
        <div class="panel-heading"><div><p class="eyebrow">Durable state machine</p><h2>Workflows</h2></div><span>{{ workflows.length }} recent</span></div>
        <div class="table-wrap"><table><thead><tr><th>Repository</th><th>Subject</th><th>State</th><th>Revision</th><th>Budget</th><th>Updated</th><th></th></tr></thead><tbody>
          <tr v-for="item in workflows" :key="item.id"><td>{{ repositoryName(item.repository_id) }}</td><td>#{{ item.subject }}</td><td><span class="badge" :class="statusClass(item.state)">{{ item.state }}</span></td><td class="mono">{{ item.revision.slice(0, 10) || '—' }}</td><td>CI {{ item.ci_repair_attempts }}/{{ item.ci_repair_limit }} · R {{ item.review_repair_attempts }}/{{ item.review_repair_limit }}</td><td>{{ formatTime(item.updated_at) }}</td><td><button class="link-button" @click="inspectWorkflow(item.id)">Detail</button></td></tr>
        </tbody></table></div>
      </section>

      <section v-else-if="view === 'jobs'" class="panel full">
        <div class="panel-heading"><div><p class="eyebrow">Execution queue</p><h2>Jobs</h2></div><span>{{ jobs.length }} recent</span></div>
        <div class="table-wrap"><table><thead><tr><th>Role</th><th>Action</th><th>Subject</th><th>Status</th><th>Attempt</th><th>Lease</th><th>Last error</th></tr></thead><tbody>
          <tr v-for="item in jobs" :key="item.id"><td>{{ item.role }}</td><td>{{ item.kind }}</td><td>#{{ item.subject }}</td><td><span class="badge" :class="statusClass(item.status)">{{ item.status }}</span></td><td>{{ item.attempt }}/{{ item.max_attempts }}</td><td>{{ item.lease_owner || '—' }}</td><td class="wide error-cell">{{ item.last_error || '—' }}</td></tr>
        </tbody></table></div>
      </section>

      <section v-else-if="view === 'runs'" class="panel full">
        <div class="panel-heading"><div><p class="eyebrow">Runtime attempts</p><h2>Runs & evidence</h2></div><span>{{ runs.length }} recent</span></div>
        <div class="table-wrap"><table><thead><tr><th>Runtime</th><th>Model</th><th>Status</th><th>Attempt</th><th>Started</th><th>Summary</th><th></th></tr></thead><tbody>
          <tr v-for="item in runs" :key="item.id"><td>{{ item.runtime }}</td><td>{{ item.model || 'default' }}</td><td><span class="badge" :class="statusClass(item.status)">{{ item.status }}</span></td><td>{{ item.attempt }}.{{ item.sequence }}</td><td>{{ formatTime(item.started_at) }}</td><td class="wide">{{ item.summary || '—' }}</td><td><button class="link-button" @click="inspectRun(item)">Evidence</button></td></tr>
        </tbody></table></div>
      </section>

      <section v-else-if="view === 'repositories'" class="panel full">
        <div class="panel-heading"><div><p class="eyebrow">Managed sources</p><h2>Repository onboarding</h2></div><span>{{ repositoryConfigs.length }} configured</span></div>
        <form class="unlock-form" @submit.prevent="registerRepository">
          <input v-model="repositoryFullName" placeholder="owner/repository" autocomplete="off" />
          <input v-model="repositoryDefaultBranch" placeholder="default branch" autocomplete="off" />
          <input v-model="repositoryIntegrationBranch" placeholder="integration branch" autocomplete="off" />
          <input v-model="repositoryWorkspacePolicy" placeholder="workspace policy (optional)" autocomplete="off" />
          <button type="submit" :disabled="repositorySaving || !repositoryFullName.trim()">{{ repositorySaving ? 'Saving…' : 'Register & validate' }}</button>
        </form>
        <p class="security-note">Activation validates GitHub access plus both required branches. Secrets are never returned by this API.</p>
        <div class="card-grid">
          <article v-for="item in repositoryConfigs" :key="item.id" class="repo-card">
            <div><span class="provider">{{ item.provider }}</span><h3>{{ item.full_name }}</h3></div>
            <dl>
              <div><dt>Default branch</dt><dd>{{ item.default_branch }}</dd></div>
              <div><dt>Integration branch</dt><dd>{{ item.integration_branch }}</dd></div>
              <div><dt>Config version</dt><dd>v{{ item.config_version }}</dd></div>
              <div><dt>Workspace policy</dt><dd>{{ item.workspace_policy || 'default' }}</dd></div>
              <div><dt>Status</dt><dd><span class="badge" :class="item.enabled ? 'good' : 'bad'">{{ item.enabled ? 'enabled' : 'disabled' }}</span></dd></div>
            </dl>
            <div class="budget-row">
              <button class="link-button" :disabled="repositorySaving" @click="toggleRepository(item)">{{ item.enabled ? 'Disable' : 'Enable & validate' }}</button>
              <button class="link-button" @click="inspectRepositoryAudit(item)">Audit</button>
            </div>
          </article>
        </div>
        <section v-if="auditedRepository" class="panel">
          <div class="panel-heading"><div><p class="eyebrow">Mutation evidence</p><h2>{{ auditedRepository.full_name }} audit</h2></div><span>{{ repositoryAudit.length }} entries</span></div>
          <div v-if="!repositoryAudit.length" class="empty">No configuration mutations recorded.</div>
          <div v-else class="table-wrap"><table><thead><tr><th>Version</th><th>Action</th><th>Actor</th><th>Created</th></tr></thead><tbody>
            <tr v-for="entry in repositoryAudit" :key="entry.id"><td>v{{ entry.config_version }}</td><td>{{ entry.action }}</td><td>{{ entry.actor }}</td><td>{{ formatTime(entry.created_at) }}</td></tr>
          </tbody></table></div>
        </section>
      </section>

      <section v-else class="panel full">
        <div class="panel-heading"><div><p class="eyebrow">Execution hosts</p><h2>Workers</h2></div><span>{{ workers.length }} registered</span></div>
        <div class="table-wrap"><table><thead><tr><th>Worker</th><th>Host</th><th>Capacity</th><th>State</th><th>Last heartbeat</th><th>Started</th></tr></thead><tbody>
          <tr v-for="item in workers" :key="item.id"><td>{{ item.id }}</td><td>{{ item.host }}</td><td>{{ item.capacity }}</td><td><span class="badge" :class="item.healthy ? 'good' : 'bad'">{{ item.draining ? 'draining' : item.healthy ? 'healthy' : 'stale' }}</span></td><td>{{ formatTime(item.last_heartbeat) }}</td><td>{{ formatTime(item.started_at) }}</td></tr>
        </tbody></table></div>
      </section>
    </section>

    <div v-if="selectedWorkflow || selectedRun" class="scrim" @click.self="closeDetail">
      <aside class="detail-drawer">
        <button class="drawer-close" @click="closeDetail">×</button>
        <template v-if="selectedWorkflow">
          <p class="eyebrow">Workflow detail</p>
          <h2>#{{ selectedWorkflow.workflow.subject }} · {{ selectedWorkflow.workflow.state }}</h2>
          <p class="muted mono">{{ selectedWorkflow.workflow.revision || 'No revision' }}</p>
          <div class="budget-row"><span>CI repair {{ selectedWorkflow.workflow.ci_repair_attempts }}/{{ selectedWorkflow.workflow.ci_repair_limit }}</span><span>Review repair {{ selectedWorkflow.workflow.review_repair_attempts }}/{{ selectedWorkflow.workflow.review_repair_limit }}</span></div>
          <h3>Actions</h3>
          <div class="timeline"><article v-for="action in selectedWorkflow.actions ?? []" :key="action.ID"><span class="dot"></span><div><strong>{{ action.Kind }}</strong><p>{{ action.Role }} · {{ action.Status }}<template v-if="action.Decision"> · {{ action.Decision }}</template></p><small>{{ formatTime(action.CreatedAt) }}</small></div></article></div>
          <h3>State history</h3>
          <div class="timeline"><article v-for="entry in selectedWorkflow.history ?? []" :key="entry.ID"><span class="dot"></span><div><strong>{{ entry.FromState }} → {{ entry.ToState }}</strong><p>{{ entry.ActorRole }} · {{ entry.Reason || 'state reconciliation' }}</p><small>{{ formatTime(entry.CreatedAt) }}</small></div></article></div>
        </template>
        <template v-else-if="selectedRun">
          <p class="eyebrow">Run evidence</p>
          <h2>{{ selectedRun.runtime }} · {{ selectedRun.status }}</h2>
          <p class="muted">{{ selectedRun.summary || 'No runtime summary.' }}</p>
          <dl class="detail-list"><div><dt>Job</dt><dd class="mono">{{ selectedRun.job_id }}</dd></div><div><dt>Attempt</dt><dd>{{ selectedRun.attempt }}.{{ selectedRun.sequence }}</dd></div><div><dt>Model</dt><dd>{{ selectedRun.model || 'runtime default' }}</dd></div><div><dt>Started</dt><dd>{{ formatTime(selectedRun.started_at) }}</dd></div></dl>
          <h3>Evidence</h3>
          <div v-if="!selectedEvidence.length" class="empty compact">No evidence records.</div>
          <div class="evidence-list"><article v-for="item in selectedEvidence" :key="item.id"><div><strong>{{ item.name }}</strong><span>{{ item.kind }}</span></div><code v-if="item.sha256">{{ item.sha256 }}</code><small>{{ formatTime(item.created_at) }}</small></article></div>
        </template>
      </aside>
    </div>
  </div>
</template>

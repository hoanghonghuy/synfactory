<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import type { Overview } from './types'

const token = ref('')
const authorized = ref(false)
const open = ref(false)
const overview = ref<Overview | null>(null)
const error = ref('')
const loading = ref(false)
let authTimer = 0
let refreshTimer = 0

function authHeaders(): HeadersInit {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (token.value) headers.Authorization = `Bearer ${token.value}`
  return headers
}

async function syncAuth(): Promise<void> {
  const nextToken = sessionStorage.getItem('synfactory.operator.token') ?? ''
  if (nextToken) {
    const changed = nextToken !== token.value || !authorized.value
    token.value = nextToken
    authorized.value = true
    if (changed) {
      overview.value = null
      error.value = ''
    }
    return
  }

  token.value = ''
  try {
    const response = await fetch('/api/v1/auth/session', {
      cache: 'no-store',
      credentials: 'same-origin',
      headers: { Accept: 'application/json' },
    })
    const nextAuthorized = response.ok
    const changed = nextAuthorized !== authorized.value
    authorized.value = nextAuthorized
    if (!nextAuthorized) {
      overview.value = null
      open.value = false
    }
    if (changed && nextAuthorized) error.value = ''
  } catch {
    authorized.value = false
    overview.value = null
    open.value = false
  }
}

async function refresh(): Promise<void> {
  if (!authorized.value || loading.value) return
  loading.value = true
  error.value = ''
  try {
    const response = await fetch('/api/v1/overview', {
      headers: authHeaders(),
      credentials: 'same-origin',
      cache: 'no-store',
    })
    if (response.status === 401) {
      authorized.value = false
      overview.value = null
      open.value = false
      return
    }
    if (!response.ok) throw new Error(`Autonomy Health unavailable (${response.status})`)
    overview.value = await response.json() as Overview
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : 'Unable to load Autonomy Health.'
  } finally {
    loading.value = false
  }
}

async function showHealth(): Promise<void> {
  open.value = true
  await refresh()
}

function percent(value?: number): string {
  return `${Math.round((value ?? 0) * 100)}%`
}

onMounted(() => {
  void syncAuth()
  authTimer = window.setInterval(() => void syncAuth(), 2000)
  refreshTimer = window.setInterval(() => {
    if (open.value && document.visibilityState === 'visible') void refresh()
  }, 5000)
})

onBeforeUnmount(() => {
  window.clearInterval(authTimer)
  window.clearInterval(refreshTimer)
})
</script>

<template>
  <button v-if="authorized && !open" class="health-launch" type="button" @click="showHealth">Autonomy Health</button>
  <section v-if="authorized && open" class="health-dock" aria-label="Autonomy Health">
    <header>
      <div>
        <small>Last 24 hours</small>
        <strong>Autonomy Health</strong>
      </div>
      <div class="health-actions">
        <button type="button" :disabled="loading" @click="refresh">{{ loading ? 'Refreshing…' : 'Refresh' }}</button>
        <button type="button" @click="open = false">Close</button>
      </div>
    </header>
    <p v-if="error" class="health-error">{{ error }}</p>
    <div v-if="overview" class="health-grid">
      <article><span>Useful work</span><strong>{{ percent(overview.stats.useful_work_ratio_24h) }}</strong><small>{{ overview.stats.completed_actions_24h }}/{{ overview.stats.workflow_actions_24h }} actions completed</small></article>
      <article :class="{ danger: overview.stats.stuck_workflows > 0 }"><span>Stuck</span><strong>{{ overview.stats.stuck_workflows }}</strong><small>non-blocked workflows idle ≥15m</small></article>
      <article :class="{ warning: overview.stats.repairing_workflows > 0 }"><span>Repairing</span><strong>{{ overview.stats.repairing_workflows }}</strong><small>CI/review repair budget in use</small></article>
      <article :class="{ danger: overview.stats.exhausted_repair_budgets > 0 }"><span>Exhausted</span><strong>{{ overview.stats.exhausted_repair_budgets }}</strong><small>parked after bounded repair</small></article>
      <article><span>Completed</span><strong>{{ overview.stats.completed_workflows_24h }}</strong><small>workflow completions</small></article>
      <article><span>Recovered</span><strong>{{ overview.stats.recovered_workflows_24h }}</strong><small>left blocked/parked state</small></article>
      <article><span>Active</span><strong>{{ overview.stats.active_workflows }}</strong><small>non-terminal workflows</small></article>
      <article :class="{ danger: overview.stats.stale_job_leases > 0 }"><span>Stale leases</span><strong>{{ overview.stats.stale_job_leases }}</strong><small>execution ownership needs recovery</small></article>
    </div>
    <p class="health-note">Signals come from durable workflow history, workflow actions, repair counters, jobs and worker state. Healthy waiting is not counted as stuck when a workflow is explicitly blocked.</p>
  </section>
</template>

<style scoped>
.health-launch {
  position: fixed;
  left: 12px;
  bottom: max(12px, env(safe-area-inset-bottom));
  z-index: 79;
  min-height: 44px;
  padding: 0 16px;
  border: 1px solid #334155;
  border-radius: 999px;
  background: #0f172a;
  color: #f8fafc;
  font-weight: 700;
}
.health-dock {
  position: fixed;
  inset: 0;
  z-index: 85;
  overflow: auto;
  padding: max(14px, env(safe-area-inset-top)) 14px max(18px, env(safe-area-inset-bottom));
  background: #07101f;
  color: #e5edf9;
}
.health-dock header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
}
.health-dock header > div:first-child { display: grid; gap: 3px; }
.health-dock header small, .health-note { color: #91a0b8; }
.health-dock header strong { font-size: 20px; }
.health-actions { display: flex; gap: 8px; }
.health-actions button {
  min-height: 44px;
  border: 1px solid #334155;
  border-radius: 9px;
  background: #111c30;
  color: inherit;
  padding: 0 12px;
}
.health-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.health-grid article {
  min-width: 0;
  padding: 14px;
  border: 1px solid #26364d;
  border-radius: 12px;
  background: #0c1728;
  display: grid;
  gap: 5px;
}
.health-grid article span { color: #9baac0; font-size: 12px; }
.health-grid article strong { font-size: 28px; }
.health-grid article small { color: #718199; line-height: 1.35; }
.health-grid article.warning { border-color: #8a6420; }
.health-grid article.danger { border-color: #8a3540; }
.health-error { color: #fda4af; overflow-wrap: anywhere; }
.health-note { margin: 14px 0 0; font-size: 12px; line-height: 1.5; }
@media (min-width: 768px) {
  .health-dock {
    inset: auto auto 18px 18px;
    width: min(620px, calc(100vw - 36px));
    max-height: min(720px, calc(100dvh - 36px));
    border: 1px solid #334155;
    border-radius: 14px;
    box-shadow: 0 24px 80px rgb(0 0 0 / 45%);
  }
  .health-grid { grid-template-columns: repeat(4, minmax(0, 1fr)); }
}
</style>

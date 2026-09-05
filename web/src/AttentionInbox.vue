<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

type AttentionItem = {
  id: string
  repository_id?: string
  workflow_id?: string
  kind: string
  severity: 'info' | 'warning' | 'critical'
  state: 'open' | 'acknowledged' | 'snoozed' | 'resolved'
  title: string
  summary: string
  assigned_to?: string
  snoozed_until?: string
  created_at: string
  updated_at: string
}

const token = ref('')
const open = ref(false)
const loading = ref(false)
const error = ref('')
const items = ref<AttentionItem[]>([])
let tokenTimer = 0
let refreshTimer = 0

const criticalCount = computed(() => items.value.filter((item) => item.severity === 'critical').length)

function authHeaders(): HeadersInit {
  return { Authorization: `Bearer ${token.value}`, 'Content-Type': 'application/json' }
}

async function loadAttention(): Promise<void> {
  if (!token.value || loading.value || document.visibilityState !== 'visible') return
  loading.value = true
  try {
    const response = await fetch('/api/v1/attention', { headers: authHeaders(), cache: 'no-store' })
    if (response.status === 404 || response.status === 503) {
      items.value = []
      return
    }
    if (response.status === 401) {
      items.value = []
      return
    }
    if (!response.ok) throw new Error(`Attention inbox unavailable (${response.status})`)
    const body = await response.json() as { items?: AttentionItem[] }
    items.value = body.items ?? []
    error.value = ''
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : 'Unable to load attention inbox.'
  } finally {
    loading.value = false
  }
}

async function act(item: AttentionItem, action: 'acknowledge' | 'snooze' | 'resolve'): Promise<void> {
  if (!token.value) return
  error.value = ''
  const actor = 'operator'
  const body = action === 'snooze'
    ? { actor, until: new Date(Date.now() + 60 * 60 * 1000).toISOString() }
    : { actor }
  try {
    const response = await fetch(`/api/v1/attention/${encodeURIComponent(item.id)}/${action}`, {
      method: 'POST',
      headers: authHeaders(),
      body: JSON.stringify(body),
    })
    if (!response.ok) {
      const payload = await response.json().catch(() => ({})) as { error?: string }
      throw new Error(payload.error || `Attention action failed (${response.status})`)
    }
    await loadAttention()
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : 'Attention action failed.'
  }
}

function formatAge(value: string): string {
  const time = new Date(value).getTime()
  if (!Number.isFinite(time)) return 'unknown age'
  const minutes = Math.max(0, Math.floor((Date.now() - time) / 60_000))
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 48) return `${hours}h ago`
  return `${Math.floor(hours / 24)}d ago`
}

function syncToken(): void {
  const next = sessionStorage.getItem('synfactory.operator.token') ?? ''
  if (next === token.value) return
  token.value = next
  items.value = []
  error.value = ''
  if (next) void loadAttention()
}

onMounted(() => {
  syncToken()
  tokenTimer = window.setInterval(syncToken, 1000)
  refreshTimer = window.setInterval(() => void loadAttention(), 5000)
})

onBeforeUnmount(() => {
  window.clearInterval(tokenTimer)
  window.clearInterval(refreshTimer)
})
</script>

<template>
  <div v-if="token" class="attention-dock">
    <button class="attention-trigger" :class="{ critical: criticalCount > 0 }" @click="open = !open">
      <span>Attention</span>
      <strong>{{ items.length }}</strong>
    </button>

    <section v-if="open" class="attention-sheet" aria-label="Operator attention inbox">
      <header>
        <div>
          <p>Human attention</p>
          <h2>Operator inbox</h2>
        </div>
        <button class="close" aria-label="Close attention inbox" @click="open = false">×</button>
      </header>

      <div class="summary">
        <span>{{ items.length }} active</span>
        <span v-if="criticalCount" class="critical-text">{{ criticalCount }} critical</span>
        <button :disabled="loading" @click="loadAttention">{{ loading ? 'Refreshing…' : 'Refresh' }}</button>
      </div>

      <p v-if="error" class="attention-error">{{ error }}</p>
      <p v-if="!loading && !items.length" class="empty">No active human-required blockers.</p>

      <div class="attention-list">
        <article v-for="item in items" :key="item.id" class="attention-card" :class="item.severity">
          <div class="meta">
            <span class="severity">{{ item.severity }}</span>
            <span>{{ item.kind.replaceAll('_', ' ') }}</span>
            <span>{{ formatAge(item.created_at) }}</span>
          </div>
          <h3>{{ item.title }}</h3>
          <p>{{ item.summary }}</p>
          <small v-if="item.repository_id">Repository {{ item.repository_id }}</small>
          <small v-if="item.assigned_to">Assigned to {{ item.assigned_to }}</small>
          <div class="actions">
            <button v-if="item.state !== 'acknowledged'" @click="act(item, 'acknowledge')">Acknowledge</button>
            <button @click="act(item, 'snooze')">Snooze 1h</button>
            <button class="resolve" @click="act(item, 'resolve')">Revalidate & resolve</button>
          </div>
        </article>
      </div>
    </section>
  </div>
</template>

<style scoped>
.attention-dock { position: fixed; right: 18px; bottom: 18px; z-index: 65; font-family: Inter, ui-sans-serif, system-ui, sans-serif; }
.attention-trigger { display: flex; gap: 10px; align-items: center; border: 1px solid #374151; border-radius: 999px; background: #111827; color: #f9fafb; padding: 10px 14px; box-shadow: 0 12px 30px rgb(0 0 0 / 24%); cursor: pointer; }
.attention-trigger strong { min-width: 24px; border-radius: 999px; background: #374151; padding: 2px 7px; text-align: center; }
.attention-trigger.critical strong { background: #b91c1c; }
.attention-sheet { position: absolute; right: 0; bottom: 52px; width: min(440px, calc(100vw - 24px)); max-height: min(70vh, 680px); overflow: hidden; border: 1px solid #374151; border-radius: 18px; background: #0f172a; color: #e5e7eb; box-shadow: 0 20px 60px rgb(0 0 0 / 38%); }
.attention-sheet header { display: flex; align-items: center; justify-content: space-between; padding: 16px 18px 10px; }
.attention-sheet header p { margin: 0 0 3px; color: #94a3b8; font-size: 11px; letter-spacing: .12em; text-transform: uppercase; }
.attention-sheet h2 { margin: 0; font-size: 20px; }
.close { border: 0; background: transparent; color: #cbd5e1; font-size: 26px; cursor: pointer; }
.summary { display: flex; gap: 10px; align-items: center; padding: 0 18px 12px; color: #94a3b8; font-size: 12px; }
.summary button { margin-left: auto; border: 1px solid #475569; border-radius: 8px; background: #1e293b; color: #e2e8f0; padding: 6px 9px; cursor: pointer; }
.critical-text { color: #fca5a5; }
.attention-error { margin: 0 18px 10px; border-radius: 8px; background: #450a0a; color: #fecaca; padding: 8px 10px; font-size: 12px; }
.empty { margin: 0; padding: 26px 18px; color: #94a3b8; text-align: center; }
.attention-list { max-height: calc(min(70vh, 680px) - 94px); overflow-y: auto; padding: 0 12px 14px; }
.attention-card { margin-top: 8px; border: 1px solid #334155; border-left: 4px solid #64748b; border-radius: 12px; background: #111827; padding: 12px; }
.attention-card.warning { border-left-color: #f59e0b; }
.attention-card.critical { border-left-color: #ef4444; }
.meta { display: flex; flex-wrap: wrap; gap: 7px; color: #94a3b8; font-size: 11px; text-transform: capitalize; }
.severity { font-weight: 700; }
.attention-card h3 { margin: 8px 0 5px; font-size: 15px; }
.attention-card p { margin: 0 0 8px; color: #cbd5e1; font-size: 13px; line-height: 1.45; }
.attention-card small { display: block; color: #64748b; font-size: 11px; overflow-wrap: anywhere; }
.actions { display: flex; flex-wrap: wrap; gap: 7px; margin-top: 11px; }
.actions button { border: 1px solid #475569; border-radius: 8px; background: #1e293b; color: #e2e8f0; padding: 7px 9px; font-size: 12px; cursor: pointer; }
.actions .resolve { border-color: #166534; background: #14532d; }
@media (max-width: 640px) {
  .attention-dock { right: 10px; bottom: 10px; left: 10px; }
  .attention-trigger { margin-left: auto; }
  .attention-sheet { position: fixed; inset: auto 0 0 0; width: 100%; max-height: 78vh; border-radius: 18px 18px 0 0; border-left: 0; border-right: 0; border-bottom: 0; }
  .attention-list { max-height: calc(78vh - 94px); padding-bottom: max(16px, env(safe-area-inset-bottom)); }
  .actions button { flex: 1 1 auto; min-height: 40px; }
}
</style>

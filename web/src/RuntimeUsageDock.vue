<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

type UsageTotals = {
  runs: number
  requests: number
  input_tokens: number
  output_tokens: number
  runtime_ms: number
  cost_microusd: number
}

type UsageItem = UsageTotals & {
  repository: string
  provider: string
  model: string
  role: string
}

type UsageResponse = {
  generated_at: string
  since: string
  repository?: string
  totals: UsageTotals
  items: UsageItem[]
}

const token = ref('')
const open = ref(false)
const loading = ref(false)
const error = ref('')
const usage = ref<UsageResponse | null>(null)
const repository = ref('')
let tokenTimer = 0
let refreshTimer = 0

const totalTokens = computed(() => (usage.value?.totals.input_tokens ?? 0) + (usage.value?.totals.output_tokens ?? 0))

function authHeaders(): HeadersInit {
  return { Authorization: `Bearer ${token.value}`, Accept: 'application/json' }
}

function money(microusd: number): string {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD', minimumFractionDigits: 4, maximumFractionDigits: 6 }).format(microusd / 1_000_000)
}

function compact(value: number): string {
  return new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 }).format(value)
}

function duration(ms: number): string {
  if (ms < 1000) return `${ms} ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)} s`
  return `${(ms / 60_000).toFixed(1)} min`
}

async function loadUsage(): Promise<void> {
  if (!token.value || loading.value || document.visibilityState !== 'visible') return
  loading.value = true
  try {
    const query = new URLSearchParams({ limit: '50' })
    if (repository.value.trim()) query.set('repository', repository.value.trim())
    const response = await fetch(`/api/v1/runtime-usage?${query.toString()}`, { headers: authHeaders(), cache: 'no-store' })
    if (response.status === 401) {
      usage.value = null
      return
    }
    if (response.status === 404 || response.status === 503) {
      usage.value = null
      error.value = 'Runtime usage reporting is not available on this control plane.'
      return
    }
    if (!response.ok) throw new Error(`Runtime usage unavailable (${response.status})`)
    usage.value = await response.json() as UsageResponse
    error.value = ''
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : 'Unable to load runtime usage.'
  } finally {
    loading.value = false
  }
}

function syncToken(): void {
  const next = sessionStorage.getItem('synfactory.operator.token') ?? ''
  if (next === token.value) return
  token.value = next
  usage.value = null
  error.value = ''
  if (next) void loadUsage()
}

onMounted(() => {
  syncToken()
  tokenTimer = window.setInterval(syncToken, 1000)
  refreshTimer = window.setInterval(() => void loadUsage(), 10_000)
})

onBeforeUnmount(() => {
  window.clearInterval(tokenTimer)
  window.clearInterval(refreshTimer)
})
</script>

<template>
  <div v-if="token" class="usage-dock">
    <button class="usage-trigger" @click="open = !open">
      <span>Cost</span>
      <strong>{{ usage ? money(usage.totals.cost_microusd) : '—' }}</strong>
    </button>

    <section v-if="open" class="usage-sheet" aria-label="Runtime usage and cost">
      <header>
        <div>
          <p>Runtime governance</p>
          <h2>Usage & cost</h2>
        </div>
        <button class="close" aria-label="Close runtime usage" @click="open = false">×</button>
      </header>

      <div class="filter-row">
        <input v-model="repository" placeholder="owner/repository (all)" @keyup.enter="loadUsage" />
        <button :disabled="loading" @click="loadUsage">{{ loading ? 'Refreshing…' : 'Refresh' }}</button>
      </div>

      <p v-if="error" class="usage-error">{{ error }}</p>

      <template v-if="usage">
        <div class="summary-grid">
          <article><span>Cost</span><strong>{{ money(usage.totals.cost_microusd) }}</strong><small>last 24 hours</small></article>
          <article><span>Runs</span><strong>{{ compact(usage.totals.runs) }}</strong><small>{{ compact(usage.totals.requests) }} requests</small></article>
          <article><span>Tokens</span><strong>{{ compact(totalTokens) }}</strong><small>{{ compact(usage.totals.input_tokens) }} in · {{ compact(usage.totals.output_tokens) }} out</small></article>
          <article><span>Runtime</span><strong>{{ duration(usage.totals.runtime_ms) }}</strong><small>aggregated execution time</small></article>
        </div>

        <div class="usage-list">
          <article v-for="item in usage.items" :key="`${item.repository}:${item.provider}:${item.model}:${item.role}`" class="usage-card">
            <div class="usage-card-head">
              <div><strong>{{ item.model || 'default model' }}</strong><span>{{ item.provider }} · {{ item.role }}</span></div>
              <b>{{ money(item.cost_microusd) }}</b>
            </div>
            <p>{{ item.repository }}</p>
            <dl>
              <div><dt>Runs</dt><dd>{{ item.runs }}</dd></div>
              <div><dt>Requests</dt><dd>{{ item.requests }}</dd></div>
              <div><dt>Tokens</dt><dd>{{ compact(item.input_tokens + item.output_tokens) }}</dd></div>
              <div><dt>Runtime</dt><dd>{{ duration(item.runtime_ms) }}</dd></div>
            </dl>
          </article>
          <p v-if="!usage.items.length" class="empty">No runtime usage in this window.</p>
        </div>
      </template>
    </section>
  </div>
</template>

<style scoped>
.usage-dock { position: fixed; right: 138px; bottom: 18px; z-index: 64; font-family: Inter, ui-sans-serif, system-ui, sans-serif; }
.usage-trigger { display: flex; gap: 10px; align-items: center; border: 1px solid #374151; border-radius: 999px; background: #111827; color: #f9fafb; padding: 10px 14px; box-shadow: 0 12px 30px rgb(0 0 0 / 24%); cursor: pointer; }
.usage-trigger strong { color: #86efac; font-size: 12px; }
.usage-sheet { position: absolute; right: 0; bottom: 52px; width: min(520px, calc(100vw - 24px)); max-height: min(76vh, 760px); overflow: hidden; border: 1px solid #374151; border-radius: 18px; background: #0f172a; color: #e5e7eb; box-shadow: 0 20px 60px rgb(0 0 0 / 38%); }
.usage-sheet header { display: flex; align-items: center; justify-content: space-between; padding: 16px 18px 10px; }
.usage-sheet header p { margin: 0 0 3px; color: #94a3b8; font-size: 11px; letter-spacing: .12em; text-transform: uppercase; }
.usage-sheet h2 { margin: 0; font-size: 20px; }
.close { border: 0; background: transparent; color: #cbd5e1; font-size: 26px; cursor: pointer; }
.filter-row { display: flex; gap: 8px; padding: 0 18px 12px; }
.filter-row input { min-width: 0; flex: 1; border: 1px solid #334155; border-radius: 9px; background: #111827; color: #e5e7eb; padding: 8px 10px; }
.filter-row button { border: 1px solid #475569; border-radius: 9px; background: #1e293b; color: #e2e8f0; padding: 8px 10px; cursor: pointer; }
.usage-error { margin: 0 18px 10px; border-radius: 8px; background: #450a0a; color: #fecaca; padding: 8px 10px; font-size: 12px; }
.summary-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; padding: 0 14px 12px; }
.summary-grid article { border: 1px solid #334155; border-radius: 12px; background: #111827; padding: 11px; }
.summary-grid span, .summary-grid small { display: block; color: #94a3b8; font-size: 11px; }
.summary-grid strong { display: block; margin: 4px 0; color: #f8fafc; font-size: 19px; }
.usage-list { max-height: calc(min(76vh, 760px) - 235px); overflow-y: auto; padding: 0 12px 14px; }
.usage-card { margin-top: 8px; border: 1px solid #334155; border-radius: 12px; background: #111827; padding: 12px; }
.usage-card-head { display: flex; gap: 12px; align-items: flex-start; justify-content: space-between; }
.usage-card-head strong, .usage-card-head span { display: block; }
.usage-card-head span, .usage-card p { color: #94a3b8; font-size: 11px; }
.usage-card-head b { color: #86efac; font-size: 13px; white-space: nowrap; }
.usage-card p { margin: 8px 0; overflow-wrap: anywhere; }
.usage-card dl { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 7px; margin: 0; }
.usage-card dl div { border-radius: 8px; background: #0f172a; padding: 7px; }
.usage-card dt { color: #64748b; font-size: 10px; }
.usage-card dd { margin: 2px 0 0; color: #e2e8f0; font-size: 12px; }
.empty { padding: 24px 8px; color: #94a3b8; text-align: center; }
@media (max-width: 640px) {
  .usage-dock { right: auto; left: 10px; bottom: 10px; }
  .usage-sheet { position: fixed; inset: auto 0 0 0; width: 100%; max-height: 82vh; border-radius: 18px 18px 0 0; border-left: 0; border-right: 0; border-bottom: 0; }
  .usage-list { max-height: calc(82vh - 235px); padding-bottom: max(16px, env(safe-area-inset-bottom)); }
  .usage-card dl { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
</style>

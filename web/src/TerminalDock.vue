<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'

type Target = { id: string; kind: 'local' | 'ssh'; host?: string; user?: string; port?: number; work_dir?: string }
type Session = { id: string; target_id: string; kind: string; opened_at: string; last_io_at: string }

const token = ref('')
const open = ref(false)
const targets = ref<Target[]>([])
const targetID = ref('')
const session = ref<Session | null>(null)
const connected = ref(false)
const error = ref('')
const output = ref('')
const terminalEl = ref<HTMLElement | null>(null)
const inputEl = ref<HTMLTextAreaElement | null>(null)
let socket: WebSocket | null = null
let resizeObserver: ResizeObserver | null = null
let tokenTimer = 0

function authHeaders(): HeadersInit {
  return { Authorization: `Bearer ${token.value}`, 'Content-Type': 'application/json' }
}

async function loadTargets(): Promise<void> {
  if (!token.value) return
  const response = await fetch('/api/v1/terminal/targets', { headers: authHeaders() })
  if (response.status === 404) {
    targets.value = []
    return
  }
  if (!response.ok) throw new Error(`Terminal targets unavailable (${response.status})`)
  const body = await response.json() as { targets?: Target[] }
  targets.value = body.targets ?? []
  if (!targetID.value && targets.value.length) targetID.value = targets.value[0].id
}

function terminalSize(): { rows: number; cols: number } {
  const element = terminalEl.value
  if (!element) return { rows: 24, cols: 80 }
  const width = Math.max(element.clientWidth - 24, 120)
  const height = Math.max(element.clientHeight - 24, 120)
  return {
    rows: Math.max(6, Math.floor(height / 18)),
    cols: Math.max(20, Math.floor(width / 8.4)),
  }
}

function sendResize(): void {
  if (!socket || socket.readyState !== WebSocket.OPEN) return
  const size = terminalSize()
  socket.send(JSON.stringify({ type: 'resize', rows: size.rows, cols: size.cols }))
}

async function connect(): Promise<void> {
  if (!token.value || !targetID.value) return
  error.value = ''
  await closeSession(false)
  const size = terminalSize()
  const response = await fetch('/api/v1/terminal/sessions', {
    method: 'POST',
    headers: authHeaders(),
    body: JSON.stringify({ target_id: targetID.value, rows: size.rows, cols: size.cols }),
  })
  if (!response.ok) {
    const body = await response.json().catch(() => ({})) as { error?: string }
    throw new Error(body.error || `Unable to open terminal (${response.status})`)
  }
  const body = await response.json() as { session: Session; stream_ticket: string }
  session.value = body.session
  output.value = ''
  const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:'
  socket = new WebSocket(`${scheme}//${location.host}/api/v1/terminal/sessions/${encodeURIComponent(body.session.id)}/stream`, `synfactory-terminal.${body.stream_ticket}`)
  socket.binaryType = 'arraybuffer'
  socket.onopen = async () => {
    connected.value = true
    sendResize()
    await nextTick()
    inputEl.value?.focus()
  }
  socket.onmessage = (event) => {
    if (typeof event.data === 'string') output.value += event.data
    else if (event.data instanceof ArrayBuffer) output.value += new TextDecoder().decode(event.data)
    void nextTick(() => {
      if (terminalEl.value) terminalEl.value.scrollTop = terminalEl.value.scrollHeight
    })
  }
  socket.onerror = () => { error.value = 'Terminal stream disconnected.' }
  socket.onclose = () => { connected.value = false }
}

async function closeSession(callAPI = true): Promise<void> {
  const current = session.value
  socket?.close()
  socket = null
  connected.value = false
  session.value = null
  if (callAPI && current && token.value) {
    await fetch(`/api/v1/terminal/sessions/${encodeURIComponent(current.id)}`, { method: 'DELETE', headers: authHeaders() }).catch(() => undefined)
  }
}

function sendInput(event: Event): void {
  const element = event.target as HTMLTextAreaElement
  if (!element.value || !socket || socket.readyState !== WebSocket.OPEN) return
  socket.send(JSON.stringify({ type: 'input', data: element.value }))
  element.value = ''
}

async function showTerminal(): Promise<void> {
  open.value = true
  error.value = ''
  try {
    await loadTargets()
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : 'Unable to load terminal targets.'
  }
  await nextTick()
  inputEl.value?.focus()
}

function hideTerminal(): void {
  open.value = false
}

function syncToken(): void {
  token.value = sessionStorage.getItem('synfactory.operator.token') ?? ''
  if (!token.value && session.value) void closeSession(false)
}

onMounted(() => {
  syncToken()
  tokenTimer = window.setInterval(syncToken, 1000)
  resizeObserver = new ResizeObserver(sendResize)
  if (terminalEl.value) resizeObserver.observe(terminalEl.value)
  window.visualViewport?.addEventListener('resize', sendResize)
  window.addEventListener('orientationchange', sendResize)
})

onBeforeUnmount(() => {
  window.clearInterval(tokenTimer)
  resizeObserver?.disconnect()
  window.visualViewport?.removeEventListener('resize', sendResize)
  window.removeEventListener('orientationchange', sendResize)
  void closeSession()
})
</script>

<template>
  <button v-if="token && !open" class="terminal-launch" type="button" @click="showTerminal">Terminal</button>
  <section v-if="token && open" class="terminal-dock" aria-label="Operator terminal">
    <header class="terminal-toolbar">
      <select v-model="targetID" :disabled="connected" aria-label="Terminal target">
        <option v-for="target in targets" :key="target.id" :value="target.id">{{ target.id }} · {{ target.kind }}</option>
      </select>
      <button v-if="!connected" type="button" :disabled="!targetID" @click="connect().catch((cause) => error = cause instanceof Error ? cause.message : 'Unable to connect')">Connect</button>
      <button v-else type="button" @click="closeSession()">Close</button>
      <button type="button" class="terminal-hide" @click="hideTerminal">Hide</button>
    </header>
    <div class="terminal-status">
      <span>{{ connected ? `Connected · ${session?.target_id}` : 'Disconnected' }}</span>
      <span v-if="error" class="terminal-error">{{ error }}</span>
    </div>
    <div ref="terminalEl" class="terminal-screen" @click="inputEl?.focus()">
      <pre>{{ output || 'Select a target and connect.' }}</pre>
      <textarea ref="inputEl" class="terminal-input" aria-label="Terminal input" autocomplete="off" autocapitalize="off" spellcheck="false" @input="sendInput" />
    </div>
  </section>
</template>

<style scoped>
.terminal-launch {
  position: fixed;
  right: 12px;
  bottom: max(12px, env(safe-area-inset-bottom));
  z-index: 80;
  min-height: 44px;
  padding: 0 18px;
  border-radius: 999px;
  border: 1px solid #334155;
  background: #0f172a;
  color: #f8fafc;
  font-weight: 700;
}
.terminal-dock {
  position: fixed;
  inset: 0;
  z-index: 90;
  display: grid;
  grid-template-rows: auto auto minmax(0, 1fr);
  height: 100dvh;
  background: #020617;
  color: #e2e8f0;
}
.terminal-toolbar {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  gap: 8px;
  padding: max(8px, env(safe-area-inset-top)) 10px 8px;
}
.terminal-toolbar select,
.terminal-toolbar button {
  min-height: 44px;
  border-radius: 8px;
  border: 1px solid #334155;
  background: #0f172a;
  color: inherit;
  padding: 0 10px;
}
.terminal-status {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 16px;
  padding: 0 12px 8px;
  font-size: 12px;
  color: #94a3b8;
}
.terminal-error { color: #fca5a5; }
.terminal-screen {
  position: relative;
  min-height: 0;
  overflow: auto;
  overscroll-behavior: contain;
  padding: 12px 12px max(12px, env(safe-area-inset-bottom));
  background: #020617;
}
.terminal-screen pre {
  margin: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font: 14px/18px ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  color: #e2e8f0;
}
.terminal-input {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
  pointer-events: none;
}
@media (min-width: 768px) {
  .terminal-dock {
    inset: auto 18px 18px auto;
    width: min(900px, calc(100vw - 36px));
    height: min(680px, calc(100dvh - 36px));
    border: 1px solid #334155;
    border-radius: 12px;
    overflow: hidden;
    box-shadow: 0 24px 80px rgb(0 0 0 / 45%);
  }
}
</style>

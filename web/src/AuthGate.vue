<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import App from './App.vue'
import { OperatorApi, OperatorApiError, type CurrentSession } from './api'

const tokenKey = 'synfactory.operator.token'
const sessionIDKey = 'synfactory.session.id'
const sessionExpiryKey = 'synfactory.session.expires_at'

const token = ref(sessionStorage.getItem(tokenKey) ?? '')
const sessionID = ref(sessionStorage.getItem(sessionIDKey) ?? '')
const session = ref<CurrentSession | null>(null)
const legacyTokenInput = ref('')
const checking = ref(Boolean(token.value && sessionID.value))
const error = ref('')

const namedSession = computed(() => Boolean(sessionID.value))
const principalName = computed(() => session.value?.principal.display_name || session.value?.principal.subject || 'Named user')

function clearSessionStorage(): void {
  sessionStorage.removeItem(tokenKey)
  sessionStorage.removeItem(sessionIDKey)
  sessionStorage.removeItem(sessionExpiryKey)
  token.value = ''
  sessionID.value = ''
  session.value = null
}

function signInWithGitHub(): void {
  window.location.assign('/api/v1/auth/github/login')
}

function unlockLegacy(): void {
  const candidate = legacyTokenInput.value.trim()
  if (!candidate) return
  sessionStorage.setItem(tokenKey, candidate)
  sessionStorage.removeItem(sessionIDKey)
  sessionStorage.removeItem(sessionExpiryKey)
  window.location.reload()
}

async function validateNamedSession(): Promise<void> {
  if (!token.value || !sessionID.value) {
    checking.value = false
    return
  }
  checking.value = true
  error.value = ''
  try {
    const current = await new OperatorApi(token.value).currentSession()
    if (current.id !== sessionID.value) throw new Error('Session identity changed unexpectedly.')
    session.value = current
  } catch (cause) {
    clearSessionStorage()
    if (cause instanceof OperatorApiError && cause.status === 401) {
      error.value = 'Your named session expired or was revoked. Sign in again.'
    } else {
      error.value = cause instanceof Error ? cause.message : 'Unable to validate the current session.'
    }
  } finally {
    checking.value = false
  }
}

async function signOut(): Promise<void> {
  error.value = ''
  if (token.value && namedSession.value) {
    try {
      await new OperatorApi(token.value).revokeCurrentSession()
    } catch (cause) {
      if (!(cause instanceof OperatorApiError && cause.status === 401)) {
        error.value = cause instanceof Error ? cause.message : 'Unable to revoke the session.'
        return
      }
    }
  }
  clearSessionStorage()
  window.location.reload()
}

onMounted(() => void validateNamedSession())
</script>

<template>
  <main v-if="checking" class="auth-shell">
    <section class="auth-card compact">
      <div class="auth-mark">SF</div>
      <p class="eyebrow">Secure control plane</p>
      <h1>Validating session…</h1>
      <p class="muted">Checking the server-owned named-user session before loading operator data.</p>
    </section>
  </main>

  <main v-else-if="!token" class="auth-shell">
    <section class="auth-card">
      <div class="auth-mark">SF</div>
      <p class="eyebrow">Named operator access</p>
      <h1>SynFactory Control Center</h1>
      <p class="muted">Sign in with an identity managed by the Go control plane. Authorization remains server-side and repository scoped.</p>
      <button class="primary-action" type="button" @click="signInWithGitHub">Sign in with GitHub</button>

      <div class="auth-divider"><span>Migration fallback</span></div>
      <form class="legacy-form" @submit.prevent="unlockLegacy">
        <label for="legacy-token">Legacy operator token</label>
        <div class="legacy-row">
          <input id="legacy-token" v-model="legacyTokenInput" type="password" autocomplete="current-password" placeholder="Operator token" />
          <button type="submit" :disabled="!legacyTokenInput.trim()">Unlock</button>
        </div>
      </form>
      <p v-if="error" class="auth-error">{{ error }}</p>
      <p class="security-note">Legacy token access remains available only as a migration path. Named sessions are the production identity path.</p>
    </section>
  </main>

  <template v-else>
    <App />
    <aside v-if="namedSession && session" class="identity-chip" aria-label="Current named-user session">
      <div>
        <span>Signed in</span>
        <strong>{{ principalName }}</strong>
      </div>
      <button type="button" @click="signOut">Sign out</button>
    </aside>
  </template>
</template>

<style scoped>
.auth-shell {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 24px;
  background: #0b1020;
  color: #f5f7ff;
}
.auth-card {
  width: min(100%, 520px);
  display: grid;
  gap: 18px;
  padding: 32px;
  border: 1px solid rgba(255,255,255,.12);
  border-radius: 20px;
  background: rgba(18,25,47,.96);
  box-shadow: 0 24px 80px rgba(0,0,0,.35);
}
.auth-card.compact { width: min(100%, 430px); }
.auth-card h1, .auth-card p { margin: 0; }
.auth-mark {
  width: 44px;
  height: 44px;
  display: grid;
  place-items: center;
  border-radius: 12px;
  background: #f5f7ff;
  color: #0b1020;
  font-weight: 800;
}
.primary-action, .legacy-row button, .identity-chip button {
  border: 0;
  border-radius: 10px;
  padding: 11px 16px;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
}
.primary-action { background: #f5f7ff; color: #0b1020; }
.auth-divider { display: flex; align-items: center; gap: 12px; color: #8994b3; font-size: 12px; text-transform: uppercase; letter-spacing: .08em; }
.auth-divider::before, .auth-divider::after { content: ''; height: 1px; flex: 1; background: rgba(255,255,255,.12); }
.legacy-form { display: grid; gap: 8px; }
.legacy-form label { font-size: 13px; color: #b6bfd7; }
.legacy-row { display: flex; gap: 8px; }
.legacy-row input {
  min-width: 0;
  flex: 1;
  padding: 11px 12px;
  border: 1px solid rgba(255,255,255,.16);
  border-radius: 10px;
  background: #0e1529;
  color: inherit;
  font: inherit;
}
.legacy-row button { background: #263451; color: #f5f7ff; }
.legacy-row button:disabled { opacity: .5; cursor: not-allowed; }
.eyebrow { color: #8ea2d9; font-size: 12px; font-weight: 700; text-transform: uppercase; letter-spacing: .1em; }
.muted, .security-note { color: #aab4cf; line-height: 1.55; }
.security-note { font-size: 13px; }
.auth-error { padding: 10px 12px; border-radius: 10px; background: rgba(180,45,57,.18); color: #ffd7db; }
.identity-chip {
  position: fixed;
  z-index: 60;
  right: 18px;
  bottom: 18px;
  display: flex;
  align-items: center;
  gap: 12px;
  max-width: calc(100vw - 36px);
  padding: 10px 12px;
  border: 1px solid rgba(255,255,255,.12);
  border-radius: 12px;
  background: rgba(11,16,32,.94);
  color: #f5f7ff;
  box-shadow: 0 12px 36px rgba(0,0,0,.3);
}
.identity-chip div { display: grid; min-width: 0; }
.identity-chip span { color: #8994b3; font-size: 11px; text-transform: uppercase; }
.identity-chip strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.identity-chip button { background: #263451; color: #f5f7ff; white-space: nowrap; }
@media (max-width: 560px) {
  .auth-shell { padding: 14px; }
  .auth-card { padding: 22px; }
  .legacy-row { display: grid; }
  .identity-chip { left: 12px; right: 12px; bottom: 12px; justify-content: space-between; }
}
</style>

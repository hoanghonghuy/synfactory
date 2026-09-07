import { createApp, nextTick } from 'vue'
import AuthGate from './AuthGate.vue'
import AttentionInbox from './AttentionInbox.vue'
import AutonomyHealth from './AutonomyHealth.vue'
import RuntimeUsageDock from './RuntimeUsageDock.vue'
import TerminalDock from './TerminalDock.vue'
import './style.css'
import './mobile-tables.css'

createApp(AuthGate).mount('#app')

function labelMobileTableCells(): void {
  document.querySelectorAll<HTMLTableElement>('.table-wrap table').forEach((table) => {
    const labels = Array.from(table.querySelectorAll<HTMLTableCellElement>('thead th')).map((cell) => cell.textContent?.trim() ?? '')
    table.querySelectorAll<HTMLTableRowElement>('tbody tr').forEach((row) => {
      Array.from(row.children).forEach((cell, index) => {
        if (cell instanceof HTMLTableCellElement) cell.dataset.label = labels[index] ?? ''
      })
    })
  })
}

const observer = new MutationObserver(() => void nextTick(labelMobileTableCells))
observer.observe(document.getElementById('app')!, { childList: true, subtree: true })
void nextTick(labelMobileTableCells)

const attentionRoot = document.createElement('div')
attentionRoot.id = 'attention-root'
document.body.appendChild(attentionRoot)
createApp(AttentionInbox).mount(attentionRoot)

const healthRoot = document.createElement('div')
healthRoot.id = 'autonomy-health-root'
document.body.appendChild(healthRoot)
createApp(AutonomyHealth).mount(healthRoot)

const usageRoot = document.createElement('div')
usageRoot.id = 'runtime-usage-root'
document.body.appendChild(usageRoot)
createApp(RuntimeUsageDock).mount(usageRoot)

const terminalRoot = document.createElement('div')
terminalRoot.id = 'terminal-root'
document.body.appendChild(terminalRoot)
createApp(TerminalDock).mount(terminalRoot)

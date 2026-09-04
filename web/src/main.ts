import { createApp } from 'vue'
import App from './App.vue'
import TerminalDock from './TerminalDock.vue'
import './style.css'

createApp(App).mount('#app')

const terminalRoot = document.createElement('div')
terminalRoot.id = 'terminal-root'
document.body.appendChild(terminalRoot)
createApp(TerminalDock).mount(terminalRoot)

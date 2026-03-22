<template>
  <section class="workspace-panel">
    <header class="workspace-panel__header">
      <div>
        <h2>{{ sessionLabel }}</h2>
        <p>{{ session.endpoint }} · {{ session.shortSessionId }}</p>
      </div>

      <div class="workspace-panel__actions">
        <el-tag size="small" :type="statusTagType">{{ statusText }}</el-tag>
        <el-button link type="primary" :disabled="rootSwitchDisabled" @click="handleRootSwitch">切换 root</el-button>
        <el-button link type="danger" @click="$emit('close', session.sessionId)">关闭会话</el-button>
      </div>
    </header>

    <el-alert
      v-if="bannerMessage"
      :title="bannerMessage"
      :type="bannerType"
      :closable="false"
      show-icon
      class="workspace-panel__banner"
    />

    <div ref="terminalHostRef" class="terminal-host"></div>
  </section>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { Terminal } from 'xterm'
import { FitAddon } from '@xterm/addon-fit'
import 'xterm/css/xterm.css'
import { TerminalRealtimeClient } from '@/utils/terminalRealtime'

const props = defineProps({
  session: {
    type: Object,
    required: true
  },
  sessionLabel: {
    type: String,
    required: true
  },
  visible: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['close', 'closed'])

const terminalHostRef = ref(null)
const connectionState = ref('idle')
const bannerMessage = ref('')
const bannerType = ref('info')

let terminal = null
let fitAddon = null
let realtimeClient = null
let resizeObserver = null
let pendingFitFrame = 0
const clientBindings = []

const statusText = computed(() => ({
  idle: '待连接',
  connecting: '连接中',
  connected: '已连接',
  ready: '已就绪',
  closed: '已关闭',
  error: '连接异常'
}[connectionState.value] || '待连接'))

const statusTagType = computed(() => ({
  idle: 'info',
  connecting: 'info',
  connected: 'success',
  ready: 'success',
  closed: 'warning',
  error: 'danger'
}[connectionState.value] || 'info'))

const rootSwitchDisabled = computed(() => !['connected', 'ready'].includes(connectionState.value))

const resolveTheme = () => {
  if (typeof window === 'undefined') {
    return {
      background: '#0b1220',
      foreground: '#e8f1ff',
      cursor: '#78a2ff',
      selectionBackground: 'rgba(120, 162, 255, 0.22)'
    }
  }

  const styles = window.getComputedStyle(document.documentElement)
  return {
    background: styles.getPropertyValue('--terminal-bg').trim() || '#0b1220',
    foreground: styles.getPropertyValue('--terminal-fg').trim() || '#e8f1ff',
    cursor: styles.getPropertyValue('--primary-color').trim() || '#78a2ff',
    selectionBackground: styles.getPropertyValue('--terminal-selection').trim() || 'rgba(120, 162, 255, 0.22)'
  }
}

const bindClientEvent = (event, handler) => {
  if (!realtimeClient) return
  realtimeClient.on(event, handler)
  clientBindings.push([event, handler])
}

const clearPendingFitFrame = () => {
  if (!pendingFitFrame) return
  cancelAnimationFrame(pendingFitFrame)
  pendingFitFrame = 0
}

const hasSaneHostSize = () => {
  if (!terminalHostRef.value || typeof window === 'undefined') {
    return false
  }

  const rect = terminalHostRef.value.getBoundingClientRect()
  const maxWidth = Math.max(window.innerWidth * 2, 1200)
  const maxHeight = Math.max(window.innerHeight * 2, 1200)

  return rect.width >= 120 && rect.height >= 120 && rect.width <= maxWidth && rect.height <= maxHeight
}

const scheduleFitTerminal = () => {
  if (!props.visible || pendingFitFrame) return

  pendingFitFrame = requestAnimationFrame(() => {
    pendingFitFrame = 0
    fitTerminal()
  })
}

const fitTerminal = () => {
  if (!props.visible || !terminal || !fitAddon) return false
  if (!hasSaneHostSize()) {
    scheduleFitTerminal()
    return false
  }

  requestAnimationFrame(() => {
    if (!hasSaneHostSize()) {
      scheduleFitTerminal()
      return
    }

    try {
      fitAddon.fit()
      if (realtimeClient?.getStatus().connected) {
        realtimeClient.sendResize({ cols: terminal.cols, rows: terminal.rows })
      }
    } catch {
      scheduleFitTerminal()
    }
  })

  return true
}

const ensureTerminal = () => {
  if (terminal || !terminalHostRef.value) return

  terminal = new Terminal({
    cursorBlink: true,
    convertEol: true,
    fontFamily: 'JetBrains Mono, Fira Code, Consolas, monospace',
    fontSize: 13,
    lineHeight: 1.35,
    scrollback: 2000,
    theme: resolveTheme()
  })
  fitAddon = new FitAddon()
  terminal.loadAddon(fitAddon)
  terminal.open(terminalHostRef.value)
  terminal.focus()
  terminal.writeln(`Connecting to ${props.session.endpoint} ...`)
  terminal.onData(data => {
    realtimeClient?.sendInput(data)
  })
  terminal.onResize(size => {
    if (hasSaneHostSize()) {
      realtimeClient?.sendResize({ cols: size.cols, rows: size.rows })
    }
  })
  resizeObserver = new ResizeObserver(() => {
    scheduleFitTerminal()
  })
  resizeObserver.observe(terminalHostRef.value)
}

const ensureRealtimeClient = () => {
  if (realtimeClient) return

  connectionState.value = 'connecting'
  realtimeClient = new TerminalRealtimeClient(props.session.sessionId)
  bindClientEvent('connected', () => {
    connectionState.value = 'connected'
    bannerMessage.value = ''
    scheduleFitTerminal()
  })
  bindClientEvent('terminal_ready', () => {
    connectionState.value = 'ready'
    bannerMessage.value = ''
    terminal?.focus()
    scheduleFitTerminal()
  })
  bindClientEvent('terminal_output', payload => {
    if (payload?.session_id !== props.session.sessionId) return
    if (payload.data) {
      terminal?.write(payload.data)
    }
  })
  bindClientEvent('terminal_error', payload => {
    if (payload?.session_id !== props.session.sessionId) return
    connectionState.value = 'error'
    bannerType.value = 'error'
    bannerMessage.value = payload?.message || payload?.error || '终端连接异常'
    if (payload?.message || payload?.error) {
      terminal?.writeln(`\r\n${payload.message || payload.error}\r\n`)
    }
  })
  bindClientEvent('terminal_closed', payload => {
    if (payload?.session_id !== props.session.sessionId) return
    connectionState.value = 'closed'
    bannerType.value = 'warning'
    bannerMessage.value = payload?.reason || '终端会话已关闭'
    emit('closed', {
      sessionId: props.session.sessionId,
      reason: payload?.reason || 'terminal_closed'
    })
  })
  bindClientEvent('disconnected', payload => {
    if (payload?.session_id !== props.session.sessionId || connectionState.value === 'closed') return
	    connectionState.value = 'connecting'
    bannerType.value = 'warning'
	    bannerMessage.value = '终端连接已断开，正在重连'
  })
  bindClientEvent('reconnecting', payload => {
    if (payload?.session_id !== props.session.sessionId || connectionState.value === 'closed') return
    connectionState.value = 'connecting'
    bannerType.value = 'warning'
    bannerMessage.value = '终端连接已断开，正在重连'
  })
  bindClientEvent('error', payload => {
    if (payload?.session_id !== props.session.sessionId) return
    connectionState.value = 'error'
    bannerType.value = 'warning'
    bannerMessage.value = '终端连接建立失败'
  })
  realtimeClient.connect()
}

watch(() => props.visible, async (visible) => {
  if (!visible) return
  await nextTick()
  ensureTerminal()
  ensureRealtimeClient()
  scheduleFitTerminal()
  terminal?.focus()
}, { immediate: true })

const handleRootSwitch = () => {
  if (!realtimeClient?.sendRootSwitch()) {
    bannerType.value = 'warning'
    bannerMessage.value = '终端暂未连接，无法切换 root'
    return
  }
  terminal?.focus()
}

onBeforeUnmount(() => {
  if (resizeObserver) {
    resizeObserver.disconnect()
  }
  clearPendingFitFrame()
  clientBindings.forEach(([event, handler]) => {
    realtimeClient?.off(event, handler)
  })
  realtimeClient?.disconnect()
  terminal?.dispose()
})
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.workspace-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  padding: $space-5;
  border-radius: $radius-2xl;
  background: var(--bg-card);
  border: 1px solid var(--border-color-light);
  box-shadow: var(--shadow-lg);
  backdrop-filter: $blur-md;
  -webkit-backdrop-filter: $blur-md;
}

.workspace-panel__header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: $space-4;
  margin-bottom: $space-4;

  h2 {
    margin: 0;
    font-family: $font-family-display;
    font-size: 22px;
    line-height: 1.2;
    font-weight: 700;
    color: var(--text-primary);
  }

  p {
    margin: $space-1 0 0;
    color: var(--text-muted);
    font-size: 13px;
  }
}

.workspace-panel__actions {
  display: flex;
  align-items: center;
  gap: $space-3;
  flex-shrink: 0;
}

.workspace-panel__banner {
  margin-bottom: $space-4;
}

.terminal-host {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  padding: $space-3;
  border-radius: $radius-lg;
  background: linear-gradient(180deg, var(--terminal-bg) 0%, var(--terminal-bg-alt) 100%);
  border: 1px solid rgba(255, 255, 255, 0.06);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.04);

  :deep(.xterm) {
    height: 100%;
  }

  :deep(.xterm-viewport) {
    overflow-y: auto;
  }

  :deep(.xterm-screen canvas) {
    border-radius: $radius-md;
  }
}

@media (max-width: 992px) {
  .workspace-panel {
    padding: $space-4;
  }

  .workspace-panel__header {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>

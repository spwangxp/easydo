<template>
  <div class="terminal-page" :class="{ 'sidebar-collapsed': isSidebarCollapsed }">
    <aside class="terminal-page__sidebar" :class="{ 'terminal-page__sidebar--collapsed': isSidebarCollapsed }">
      <button
        class="terminal-page__sidebar-toggle"
        type="button"
        :aria-label="sidebarToggleLabel"
        :title="sidebarToggleLabel"
        @click="toggleSidebar"
      >
        <span class="terminal-page__sidebar-toggle-icon" aria-hidden="true">{{ isSidebarCollapsed ? '›' : '‹' }}</span>
      </button>

      <div v-if="!isSidebarCollapsed" class="terminal-page__sidebar-content">
        <TerminalSessionList
          :sessions="decoratedSessions"
          :active-session-id="activeSessionId"
          :closing-session-id="closingSessionId"
          @select="handleSessionSelect"
          @close="handleSessionClose"
        />

        <TerminalResourceList
          :resources="vmResources"
          :active-resource-id="activeResourceId"
          :creating-resource-id="creatingResourceId"
          @open="handleResourceOpen"
        />
      </div>
    </aside>

    <main class="terminal-page__workspace">
      <div v-if="decoratedSessions.length === 0" class="terminal-empty-state">
        <h1>Web Terminal</h1>
        <p>从左侧 VM 资源列表中选择一台机器，即可在当前页面打开新的终端会话。</p>
      </div>

      <template v-else>
        <div
          v-for="session in decoratedSessions"
          v-show="session.sessionId === activeSessionId"
          :key="session.sessionId"
          class="terminal-page__workspace-panel"
        >
          <TerminalWorkspacePanel
            :session="session"
            :session-label="session.label"
            :visible="session.sessionId === activeSessionId"
            @close="handleSessionClose"
            @closed="handleSessionClosed"
          />
        </div>
      </template>
    </main>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { getResourceList } from '@/api/resource'
import {
  closeResourceTerminalSession,
  createResourceTerminalSession,
  getResourceTerminalSession,
  listResourceTerminalSessions
} from '@/api/terminal'
import TerminalResourceList from './components/TerminalResourceList.vue'
import TerminalSessionList from './components/TerminalSessionList.vue'
import TerminalWorkspacePanel from './components/TerminalWorkspacePanel.vue'
import {
  buildSessionLabel,
  closeTerminalSessionsOnPageUnload,
  normalizeTerminalSession,
  pickNextActiveSessionId,
  upsertTerminalSession
} from './terminalPageState'

const route = useRoute()
const resources = ref([])
const sessionEntries = ref([])
const activeSessionId = ref('')
const creatingResourceId = ref(0)
const closingSessionId = ref('')
const isSidebarCollapsed = ref(false)

const normalizeResource = (resource = {}) => ({
  id: Number(resource.id || 0),
  name: resource.name || '',
  type: String(resource.type || 'vm').trim().toLowerCase(),
  endpoint: resource.endpoint || '',
  status: resource.status || '',
  environment: resource.environment || ''
})

const resourcesById = computed(() => {
  return resources.value.reduce((accumulator, resource) => {
    accumulator[resource.id] = resource
    return accumulator
  }, {})
})

const vmResources = computed(() => resources.value.filter(resource => resource.type === 'vm'))

const decoratedSessions = computed(() => {
  return sessionEntries.value.map(session => ({
    ...session,
    label: buildSessionLabel(session, sessionEntries.value)
  }))
})

const activeResourceId = computed(() => {
  const activeSession = decoratedSessions.value.find(session => session.sessionId === activeSessionId.value)
  return activeSession?.resourceId || 0
})

const handlePageUnload = () => {
  closeTerminalSessionsOnPageUnload(sessionEntries.value)
}

const sidebarToggleLabel = computed(() => (isSidebarCollapsed.value ? '展开侧栏' : '折叠侧栏'))

const fetchResources = async () => {
  const response = await getResourceList()
  resources.value = Array.isArray(response?.data) ? response.data.map(item => normalizeResource(item)) : []
}

const mergeListedSessions = async (resourceId) => {
  const response = await listResourceTerminalSessions(resourceId)
  const listedSessions = Array.isArray(response?.data) ? response.data : []
  listedSessions
    .filter(session => session?.status === 'active')
    .map(session => normalizeTerminalSession(session, resourcesById.value))
    .forEach(session => {
      sessionEntries.value = upsertTerminalSession(sessionEntries.value, session)
    })
}

const appendSession = (sessionPayload) => {
  const normalized = normalizeTerminalSession(sessionPayload, resourcesById.value)
  sessionEntries.value = upsertTerminalSession(sessionEntries.value, normalized)
  activeSessionId.value = normalized.sessionId
}

const createSessionForResource = async (resourceId) => {
  const resource = resourcesById.value[resourceId]
  if (!resource || resource.type !== 'vm') {
    ElMessage.warning('当前资源不是可用的 VM 资源')
    return
  }

  creatingResourceId.value = resourceId
  try {
    await mergeListedSessions(resourceId)
    const created = await createResourceTerminalSession(resourceId)
    const sessionId = created?.data?.session_id
    const detail = sessionId
      ? await getResourceTerminalSession(resourceId, sessionId)
      : created
    appendSession(detail?.data || created?.data || {})
  } catch (error) {
    ElMessage.error(error?.response?.data?.message || error?.message || '创建终端会话失败')
  } finally {
    creatingResourceId.value = 0
  }
}

const removeSession = (sessionId) => {
  const nextActiveSessionId = pickNextActiveSessionId(sessionEntries.value, sessionId, activeSessionId.value)
  sessionEntries.value = sessionEntries.value.filter(session => session.sessionId !== sessionId)
  activeSessionId.value = nextActiveSessionId
}

const handleResourceOpen = async (resourceId) => {
  await createSessionForResource(resourceId)
}

const handleSessionSelect = (sessionId) => {
  activeSessionId.value = sessionId
}

const toggleSidebar = () => {
  isSidebarCollapsed.value = !isSidebarCollapsed.value
}

const handleSessionClose = async (sessionId) => {
  const session = sessionEntries.value.find(item => item.sessionId === sessionId)
  if (!session) return

  closingSessionId.value = sessionId
  try {
    await closeResourceTerminalSession(session.resourceId, sessionId, { reason: 'user_closed' })
    removeSession(sessionId)
  } catch (error) {
    ElMessage.error(error?.response?.data?.message || error?.message || '关闭终端会话失败')
  } finally {
    closingSessionId.value = ''
  }
}

const handleSessionClosed = ({ sessionId }) => {
  if (!sessionEntries.value.some(session => session.sessionId === sessionId)) {
    return
  }
  removeSession(sessionId)
}

const openInitialResourceSession = async () => {
  const resourceId = Number(route.query.resourceId || 0)
  if (!resourceId) {
    return
  }

  if (!resourcesById.value[resourceId]) {
    ElMessage.warning('未找到目标 VM 资源，已加载当前工作空间资源列表')
    return
  }

  await createSessionForResource(resourceId)
}

onMounted(async () => {
  if (typeof window !== 'undefined') {
    window.addEventListener('pagehide', handlePageUnload)
    window.addEventListener('beforeunload', handlePageUnload)
  }
  await fetchResources()
  await openInitialResourceSession()
})

onBeforeUnmount(() => {
  if (typeof window !== 'undefined') {
    window.removeEventListener('pagehide', handlePageUnload)
    window.removeEventListener('beforeunload', handlePageUnload)
  }
})
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.terminal-page {
  display: grid;
  grid-template-columns: 360px minmax(0, 1fr);
  gap: $space-5;
  height: 100%;
  min-height: 0;
  padding: $space-5;
  overflow: hidden;
  align-items: stretch;
}

.terminal-page__sidebar {
  position: relative;
  height: 100%;
  min-height: 0;
  overflow: visible;
}

.terminal-page__sidebar-content {
  display: grid;
  grid-template-rows: minmax(0, 1fr) minmax(0, 1fr);
  gap: $space-5;
  height: 100%;
  min-height: 0;
  overflow: hidden;
}

.terminal-page__sidebar-toggle {
  position: absolute;
  top: 50%;
  right: 0;
  z-index: 2;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 72px;
  padding: 0;
  border: 1px solid var(--border-color-light);
  border-radius: 999px;
  background: var(--bg-card);
  color: var(--text-secondary);
  font-size: 22px;
  line-height: 1;
  cursor: pointer;
  box-shadow: var(--shadow-md);
  transform: translate(50%, -50%);
  transition: border-color $transition-fast, color $transition-fast, background $transition-fast, transform $transition-fast;

  &:hover {
    color: var(--primary-color);
    border-color: var(--border-color-hover);
    background: var(--bg-card-elevated, var(--bg-card));
    transform: translate(50%, -50%) scale(1.02);
  }
}

.terminal-page__sidebar-toggle-icon {
  font-weight: 700;
  transform: translateX(-1px);
}

.sidebar-collapsed {
  grid-template-columns: 28px minmax(0, 1fr);
}

.terminal-page__sidebar--collapsed {
  min-height: 72px;
}

.terminal-page__workspace {
  min-width: 0;
  height: 100%;
  min-height: 0;
  overflow: hidden;
}

.terminal-page__workspace-panel {
  height: 100%;
  min-height: 0;
  overflow: hidden;
}

.terminal-empty-state {
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  gap: $space-3;
  height: 100%;
  min-height: calc(100vh - #{$space-10});
  padding: $space-8;
  border-radius: $radius-2xl;
  border: 1px solid var(--border-color-light);
  background: var(--bg-card);
  box-shadow: var(--shadow-lg);
  text-align: center;

  h1 {
    margin: 0;
    font-family: $font-family-display;
    font-size: 32px;
    line-height: 1.1;
    font-weight: 760;
    color: var(--text-primary);
  }

  p {
    margin: 0;
    max-width: 420px;
    color: var(--text-muted);
    line-height: 1.7;
  }
}

@media (max-width: 1200px) {
  .terminal-page {
    grid-template-columns: 320px minmax(0, 1fr);
  }
}

@media (max-width: 992px) {
  .terminal-page {
    grid-template-columns: 1fr;
    grid-template-rows: auto minmax(0, 1fr);
    height: auto;
    overflow: visible;
  }

  .sidebar-collapsed {
    grid-template-columns: 1fr;
  }

  .terminal-page__sidebar {
    height: auto;
    overflow: visible;
  }

  .terminal-page__sidebar-content {
    grid-template-rows: repeat(2, minmax(280px, auto));
    height: auto;
    overflow: visible;
  }

  .terminal-page__sidebar-toggle {
    top: $space-5;
    right: $space-4;
    width: 40px;
    height: 40px;
    transform: none;

    &:hover {
      transform: scale(1.02);
    }
  }

  .terminal-page__sidebar--collapsed {
    min-height: 56px;
  }

  .terminal-empty-state {
    min-height: 420px;
  }
}
</style>

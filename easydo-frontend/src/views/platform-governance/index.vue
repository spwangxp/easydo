<template>
  <div class="platform-governance-page">
    <div class="page-header">
      <div>
        <h1 class="page-title">平台治理</h1>
        <p class="page-subtitle">
          当前工作区：{{ userStore.currentWorkspace?.name || '-' }} · 当前工作区类型：{{ workspaceKindText }}
        </p>
      </div>
    </div>

    <el-tabs v-model="activeTab" class="governance-tabs">
      <el-tab-pane label="平台用户" name="platform-users">
        <PlatformUserManagement />
      </el-tab-pane>
      <el-tab-pane label="工作区管理" name="workspaces">
        <WorkspaceCatalogManagement />
      </el-tab-pane>
      <el-tab-pane label="模型与供应商" name="models-providers">
        <PlatformModelManagement />
      </el-tab-pane>
      <el-tab-pane label="默认运行策略" name="runtime-policies">
        <PlatformRuntimePolicyManagement />
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useUserStore } from '@/stores/user'
import PlatformUserManagement from './components/PlatformUserManagement.vue'
import WorkspaceCatalogManagement from './components/WorkspaceCatalogManagement.vue'
import PlatformModelManagement from './components/PlatformModelManagement.vue'
import PlatformRuntimePolicyManagement from './components/PlatformRuntimePolicyManagement.vue'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()
const governanceTabs = ['platform-users', 'workspaces', 'models-providers', 'runtime-policies']
const activeTab = ref(normalizeGovernanceTab(route.query.tab))

const workspaceKindText = computed(() => {
  if (userStore.currentWorkspaceKind === 'admin') {
    return 'Admin Workspace'
  }
  if (userStore.currentWorkspaceKind === 'normal') {
    return 'Normal Workspace'
  }
  return '-'
})

watch(() => route.query.tab, (tab) => {
  const nextTab = normalizeGovernanceTab(tab)
  if (activeTab.value !== nextTab) {
    activeTab.value = nextTab
  }
}, { immediate: true })

watch(activeTab, (tab) => {
  const nextTab = normalizeGovernanceTab(tab)
  const currentTab = normalizeGovernanceTab(route.query.tab)
  const hasExplicitTab = Array.isArray(route.query.tab) ? route.query.tab.length > 0 : route.query.tab != null
  if (currentTab === nextTab && (nextTab !== 'platform-users' || !hasExplicitTab)) {
    return
  }
  const query = { ...route.query }
  if (nextTab === 'platform-users') {
    delete query.tab
  } else {
    query.tab = nextTab
  }
  router.replace({ query })
})

function normalizeGovernanceTab(tab) {
  const raw = Array.isArray(tab) ? tab[0] : tab
  const normalized = String(raw || '').trim()
  return governanceTabs.includes(normalized) ? normalized : 'platform-users'
}
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.platform-governance-page {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
}

.page-title {
  margin: 0;
  font-size: 24px;
  font-weight: 600;
  color: var(--text-primary);
}

.page-subtitle {
  margin: 8px 0 0;
  color: var(--text-secondary);
  font-size: 14px;
}

.governance-tabs {
  :deep(.el-tabs__content) {
    padding-top: 12px;
  }
}
</style>

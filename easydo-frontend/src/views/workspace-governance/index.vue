<template>
  <div class="workspace-governance-page">
    <div class="page-header">
      <p class="page-subtitle">
        当前工作区：{{ userStore.currentWorkspace?.name || '-' }} · 当前角色：{{ roleText(userStore.currentWorkspace?.role) }}
      </p>
    </div>

    <div v-if="!userStore.currentWorkspaceId" class="empty-hint">请先在顶部切换到一个工作空间</div>

    <el-tabs v-else v-model="activeTab" class="governance-tabs">
      <el-tab-pane label="成员管理" name="members">
        <MemberManagement />
      </el-tab-pane>
      <el-tab-pane label="邀请管理" name="invitations">
        <InvitationManagement />
      </el-tab-pane>
      <el-tab-pane label="AI Agent" name="agents">
        <WorkspaceAgentManagement />
      </el-tab-pane>
      <el-tab-pane label="运行策略" name="runtime-profiles">
        <RuntimeProfileManagement />
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useUserStore } from '@/stores/user'
import MemberManagement from './components/MemberManagement.vue'
import InvitationManagement from './components/InvitationManagement.vue'
import WorkspaceAgentManagement from './components/WorkspaceAgentManagement.vue'
import RuntimeProfileManagement from './components/RuntimeProfileManagement.vue'

const userStore = useUserStore()
const activeTab = ref('members')

const roleText = (role) => {
  const map = {
    viewer: 'Viewer',
    developer: 'Developer',
    maintainer: 'Maintainer',
    owner: 'Owner'
  }
  return map[role] || role || '-'
}
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.workspace-governance-page {
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

.page-subtitle {
  margin: 0;
  color: var(--text-secondary);
  font-size: 14px;
}

.empty-hint {
  padding: 32px;
  background: var(--bg-card);
  border-radius: $radius-lg;
  color: var(--text-secondary);
  text-align: center;
}

.governance-tabs {
  :deep(.el-tabs__content) {
    padding-top: 12px;
  }
}
</style>

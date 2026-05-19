<template>
  <div class="settings-container">
    <div class="settings-layout">
      <aside class="settings-sidebar">
        <div 
          v-for="item in menuItems" 
          :key="item.key"
          class="menu-item"
          :class="{ active: activeMenu === item.key }"
          @click="activeMenu = item.key"
        >
          <el-icon><component :is="item.icon" /></el-icon>
          <span>{{ item.name }}</span>
        </div>
      </aside>
      
      <main class="settings-content">
        <!-- 基本设置 -->
        <div v-if="activeMenu === 'basic'" class="settings-section">
          <h2 class="section-title">基本设置</h2>
          
          <div class="form-group">
            <label>系统名称</label>
            <el-input v-model="settings.systemName" placeholder="请输入系统名称" />
          </div>
          
          <div class="form-group">
            <label>系统 Logo</label>
            <div class="logo-upload">
              <el-icon :size="40"><Upload /></el-icon>
              <span>点击上传 Logo</span>
            </div>
          </div>
          
          <div class="form-group">
            <label>系统主题</label>
            <el-radio-group v-model="settings.theme">
              <el-radio-button label="light">浅色主题</el-radio-button>
              <el-radio-button label="dark">深色主题</el-radio-button>
            </el-radio-group>
          </div>
          
          <div class="form-actions">
            <el-button type="primary" @click="saveSettings">保存设置</el-button>
          </div>
        </div>
        
        <!-- 安全设置 -->
        <div v-if="activeMenu === 'security'" class="settings-section">
          <h2 class="section-title">安全设置</h2>
          
          <div class="security-item">
            <div class="security-info">
              <h4>登录密码</h4>
              <p>定期修改密码可以提高账户安全性</p>
            </div>
            <el-button @click="showPasswordDialog = true">修改</el-button>
          </div>
          
          <div class="security-item">
            <div class="security-info">
              <h4>两步验证</h4>
              <p>开启两步验证后，登录时需要输入验证码</p>
            </div>
            <el-switch v-model="settings.twoFactorEnabled" />
          </div>
          
          <div class="security-item">
            <div class="security-info">
              <h4>登录设备管理</h4>
              <p>查看和管理已登录的设备</p>
            </div>
            <el-button @click="showDevicesDialog = true">查看</el-button>
          </div>
        </div>
        
        <!-- 通知设置 -->
        <div v-if="activeMenu === 'notifications'" class="settings-section">
          <h2 class="section-title">通知设置</h2>
          <div v-if="!userStore.currentWorkspaceId" class="empty-hint">请先在顶部切换到一个工作空间</div>

          <div v-else v-loading="notificationPreferencesLoading" class="notification-group">
            <div class="notification-scope-caption">
              当前工作空间：{{ userStore.currentWorkspace?.name || '-' }}。未单独配置的通知项会继承个人中心中的默认通知偏好。
            </div>

            <div class="notification-module-tabs">
              <button
                v-for="group in notificationEventGroups"
                :key="group.value"
                type="button"
                class="module-tab"
                :class="{ active: activeNotificationModule === group.value }"
                @click="activeNotificationModule = group.value"
              >
                <span class="module-tab-label">{{ group.label }}</span>
                <span class="module-tab-count">{{ group.events.length }} 个事件</span>
              </button>
            </div>

            <div v-if="currentNotificationGroup" class="notification-module-panel">
              <div class="notification-family-header">
                <span class="notification-family-label">{{ currentNotificationGroup.label }}</span>
                <span class="notification-family-desc">{{ currentNotificationGroup.description }}</span>
                <span v-if="currentNotificationGroup.supports_resource_scope" class="scope-hint">{{ currentNotificationGroup.resource_scope_label }}</span>
              </div>

              <div class="notification-grid-header">
                <span></span>
                <span
                  v-for="channel in notificationChannels"
                  :key="channel.value"
                  class="notification-channel"
                >
                  {{ channel.label }}
                </span>
              </div>

              <div
                v-for="event in currentNotificationGroup.events"
                :key="event.value"
                class="notification-item"
              >
                <div class="notification-info">
                  <span class="notification-label">{{ event.label }}</span>
                  <span class="notification-desc">{{ event.description }}</span>
                </div>

                <div class="notification-switch">
                  <el-switch
                    :model-value="getWorkspacePreferenceEnabled(event.value, 'in_app')"
                    :loading="isPreferenceSaving(userStore.currentWorkspaceId, event.value, 'in_app')"
                    @change="(value) => updateWorkspacePreference(event, 'in_app', value)"
                  />
                </div>

                <div class="notification-switch">
                  <el-switch
                    :model-value="getWorkspacePreferenceEnabled(event.value, 'email')"
                    :loading="isPreferenceSaving(userStore.currentWorkspaceId, event.value, 'email')"
                    @change="(value) => updateWorkspacePreference(event, 'email', value)"
                  />
                </div>
              </div>
            </div>
          </div>
        </div>
        
        <!-- 工作空间成员 -->
        <div v-if="activeMenu === 'users'" class="settings-section">
          <h2 class="section-title">工作空间成员</h2>

          <div v-if="!userStore.currentWorkspaceId" class="empty-hint">请先在顶部切换到一个工作空间</div>

          <template v-else>
            <div class="users-header">
              <div>
                <div class="workspace-name">{{ userStore.currentWorkspace?.name }}</div>
                <div class="workspace-role">当前角色：{{ roleText(userStore.currentWorkspace?.role) }}</div>
              </div>
            </div>

            <el-table :data="members" style="width: 100%">
              <el-table-column prop="username" label="用户名" width="150" />
              <el-table-column prop="email" label="邮箱" min-width="220" />
              <el-table-column prop="role" label="角色" width="180">
                <template #default="{ row }">
                  <el-tag size="small">{{ roleText(row.role) }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="status" label="状态" width="120">
                <template #default="{ row }">
                  <el-tag :type="row.status === 'active' ? 'success' : 'info'" size="small">
                    {{ row.status === 'active' ? '已启用' : '已禁用' }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="created_at" label="加入时间" width="180">
                <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
              </el-table-column>
            </el-table>
          </template>
        </div>
        
        <!-- 第三方集成 -->
        <div v-if="activeMenu === 'integrations'" class="settings-section">
          <h2 class="section-title">第三方集成</h2>
          
          <div class="integration-list">
            <div class="integration-item">
              <div class="integration-icon dingtalk">
                <el-icon :size="24"><ChatDotRound /></el-icon>
              </div>
              <div class="integration-info">
                <h4>钉钉</h4>
                <p>集成钉钉机器人，接收构建通知</p>
              </div>
              <el-button type="primary">配置</el-button>
            </div>
            
            <div class="integration-item">
              <div class="integration-icon wechat">
                <el-icon :size="24"><ChatLineRound /></el-icon>
              </div>
              <div class="integration-info">
                <h4>企业微信</h4>
                <p>集成企业微信机器人，接收构建通知</p>
              </div>
              <el-button type="primary">配置</el-button>
            </div>
            
            <div class="integration-item">
              <div class="integration-icon ldap">
                <el-icon :size="24"><Key /></el-icon>
              </div>
              <div class="integration-info">
                <h4>LDAP</h4>
                <p>集成 LDAP 统一身份认证</p>
              </div>
              <el-button type="primary">配置</el-button>
            </div>
          </div>
        </div>

      </main>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useUserStore } from '@/stores/user'
import {
  NOTIFICATION_CHANNELS,
  NOTIFICATION_EVENT_GROUPS,
  listNotificationPreferences,
  resolveNotificationPreferenceEnabled,
  upsertNotificationPreference,
  upsertNotificationPreferenceInList
} from '@/api/notification'
import { getWorkspaceMembers } from '@/api/workspace'
import {
  Setting,
  Lock,
  Bell,
  User,
  Link,
  Upload,
  ChatDotRound,
  ChatLineRound,
  Key
} from '@element-plus/icons-vue'

const userStore = useUserStore()
const activeMenu = ref('basic')
const members = ref([])
const notificationPreferences = ref([])
const notificationPreferencesLoading = ref(false)
const notificationSavingKeys = ref([])
const activeNotificationModule = ref(NOTIFICATION_EVENT_GROUPS[0]?.value || '')
const settings = reactive({
  systemName: 'EasyDo',
  theme: 'light',
  twoFactorEnabled: false
})
const showPasswordDialog = ref(false)
const showDevicesDialog = ref(false)

const notificationEventGroups = computed(() => {
  return NOTIFICATION_EVENT_GROUPS.map(group => ({
    ...group,
    events: group.events.map(event => ({ ...event, family: group.value }))
  }))
})
const notificationChannels = NOTIFICATION_CHANNELS

const menuItems = [
  { key: 'basic', name: '基本设置', icon: Setting },
  { key: 'security', name: '安全设置', icon: Lock },
  { key: 'notifications', name: '通知设置', icon: Bell },
  { key: 'users', name: '工作空间成员', icon: User },
  { key: 'integrations', name: '第三方集成', icon: Link }
]

const roleText = (role) => {
  const map = {
    viewer: 'Viewer',
    developer: 'Developer',
    maintainer: 'Maintainer',
    owner: 'Owner'
  }
  return map[role] || role || '-'
}

const formatDateTime = (timestamp) => {
  if (!timestamp) return '-'
  const date = new Date(Number(timestamp) * 1000)
  return Number.isNaN(date.getTime()) ? '-' : date.toLocaleString('zh-CN')
}

const loadWorkspaceMembers = async () => {
  if (!userStore.currentWorkspaceId) {
    members.value = []
    return
  }
  try {
    const memberRes = await getWorkspaceMembers(userStore.currentWorkspaceId)
    const memberList = memberRes?.data?.list || []
    members.value = userStore.isPlatformAdmin
      ? memberList
      : memberList.filter(member => String(member.system_role || '').toLowerCase() !== 'admin')
  } catch (error) {
    ElMessage.error('加载工作空间成员失败')
  }
}

const saveSettings = () => {
  ElMessage.success('当前阶段未实现基础设置保存')
}

const getPreferenceSavingKey = (workspaceId, eventType, channel) => {
  return `${workspaceId ?? 'global'}:${eventType}:${channel}`
}

const setPreferenceSaving = (savingKey, isSaving) => {
  if (isSaving) {
    if (!notificationSavingKeys.value.includes(savingKey)) {
      notificationSavingKeys.value = [...notificationSavingKeys.value, savingKey]
    }
    return
  }
  notificationSavingKeys.value = notificationSavingKeys.value.filter(item => item !== savingKey)
}

const isPreferenceSaving = (workspaceId, eventType, channel) => {
  return notificationSavingKeys.value.includes(getPreferenceSavingKey(workspaceId, eventType, channel))
}

const currentNotificationGroup = computed(() => {
  return notificationEventGroups.value.find(group => group.value === activeNotificationModule.value) || notificationEventGroups.value[0] || null
})

const getWorkspacePreferenceEnabled = (eventType, channel) => {
  return resolveNotificationPreferenceEnabled(notificationPreferences.value, {
    eventType,
    channel,
    workspaceId: userStore.currentWorkspaceId || null,
    fallbackToGlobal: true,
    defaultEnabled: true
  })
}

const loadNotificationPreferences = async () => {
  if (!userStore.currentWorkspaceId) {
    notificationPreferences.value = []
    return
  }

  notificationPreferencesLoading.value = true
  try {
    const res = await listNotificationPreferences({ workspace_id: userStore.currentWorkspaceId })
    if (res.code === 200) {
      notificationPreferences.value = res.data?.list || []
      return
    }
    ElMessage.error(res.message || '加载通知设置失败')
  } catch (error) {
    ElMessage.error('加载通知设置失败')
  } finally {
    notificationPreferencesLoading.value = false
  }
}

const updateWorkspacePreference = async (event, channel, enabled) => {
  if (!userStore.currentWorkspaceId) {
    return
  }

  const workspaceId = Number(userStore.currentWorkspaceId)
  const savingKey = getPreferenceSavingKey(workspaceId, event.value, channel)
  setPreferenceSaving(savingKey, true)
  try {
    const res = await upsertNotificationPreference({
      workspace_id: workspaceId,
      family: event.family,
      event_type: event.value,
      channel,
      enabled
    })
    if (res.code === 200) {
      notificationPreferences.value = upsertNotificationPreferenceInList(notificationPreferences.value, res.data)
      ElMessage.success('工作空间通知设置已更新')
      return
    }
    ElMessage.error(res.message || '更新通知设置失败')
  } catch (error) {
    ElMessage.error('更新通知设置失败')
  } finally {
    setPreferenceSaving(savingKey, false)
  }
}

watch(() => [activeMenu.value, userStore.currentWorkspaceId], async ([menu]) => {
  if (menu === 'users') {
    await loadWorkspaceMembers()
  }
  if (menu === 'notifications') {
    await loadNotificationPreferences()
  }
}, { immediate: true })
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.settings-container {
  .settings-layout {
    display: flex;
    gap: 24px;
    
    // ============================================
    // Modern Settings Sidebar
    // ============================================
    .settings-sidebar {
      width: 220px;
      background: var(--bg-card);
      border-radius: $radius-xl;
      padding: 16px;
      flex-shrink: 0;
      box-shadow: $shadow-md;
      
      .menu-item {
        display: flex;
        align-items: center;
        gap: 12px;
        padding: 14px 18px;
        color: var(--text-secondary);
        cursor: pointer;
        border-radius: $radius-md;
        transition: all $transition-base;
        font-weight: 500;
        margin-bottom: 4px;
        
        &:hover {
          background: var(--primary-lighter);
          color: var(--primary-color);
        }
        
        &.active {
          color: var(--primary-color);
          background: linear-gradient(135deg, var(--primary-light) 0%, var(--primary-lighter) 100%);
          box-shadow: inset 0 0 0 1px var(--border-color-hover), $shadow-sm;
        }
        
        .el-icon {
          font-size: 18px;
        }
      }
    }
    
    // ============================================
    // Modern Settings Content
    // ============================================
    .settings-content {
      flex: 1;
      background: var(--bg-card);
      border-radius: $radius-xl;
      padding: 32px;
      box-shadow: $shadow-md;
      
      .settings-section {
        .section-title {
          font-family: $font-family-display;
          font-size: 20px;
          font-weight: 600;
          color: var(--text-primary);
          margin-bottom: 28px;
          padding-bottom: 20px;
          border-bottom: 1px solid var(--border-color);
        }
      }
      
      .form-group {
        margin-bottom: 28px;
        
        label {
          display: block;
          font-size: 14px;
          color: var(--text-secondary);
          margin-bottom: 10px;
          font-weight: 500;
        }
        
        :deep(.el-input__wrapper) {
          background: var(--bg-secondary);
          border-radius: $radius-md;
          box-shadow: $shadow-inset;
          border: 1px solid var(--border-color-light);
          
          &:hover, &.is-focus {
            border-color: var(--border-color-hover);
          }
        }
        
        :deep(.el-radio-group) {
          .el-radio-button {
            &:first-child .el-radio-button__inner {
              border-radius: $radius-md 0 0 $radius-md;
            }
            &:last-child .el-radio-button__inner {
              border-radius: 0 $radius-md $radius-md 0;
            }
            
            .el-radio-button__inner {
              background: var(--bg-secondary);
              border-color: var(--border-color);
              color: var(--text-secondary);
              font-weight: 500;
            }
            
            &.is-active .el-radio-button__inner {
              background: linear-gradient(135deg, var(--primary-color) 0%, var(--primary-hover) 100%);
              border-color: var(--primary-color);
              color: white;
              box-shadow: none;
            }
          }
        }
      }
      
      .logo-upload {
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        width: 120px;
        height: 120px;
        border: 2px dashed var(--border-color);
        border-radius: $radius-lg;
        cursor: pointer;
        color: var(--text-muted);
        transition: all $transition-base;
        background: var(--bg-secondary);
        
        &:hover {
          border-color: var(--primary-color);
          color: var(--primary-color);
          background: var(--primary-lighter);
        }
      }
      
      .form-actions {
        margin-top: 32px;
        padding-top: 24px;
        border-top: 1px solid var(--border-color);
        
        :deep(.el-button--primary) {
          height: 44px;
          padding: 0 32px;
          border-radius: $radius-md;
          font-weight: 600;
          background: linear-gradient(135deg, var(--primary-color) 0%, var(--primary-hover) 100%);
          border: none;
          box-shadow: $shadow-md;
          
          &:hover {
            transform: translateY(-2px);
            box-shadow: $shadow-lg;
          }
        }
      }
      
      // ============================================
      // Security Items
      // ============================================
      .security-item {
        display: flex;
        align-items: center;
        justify-content: space-between;
        padding: 24px 0;
        border-bottom: 1px solid var(--border-color-light);
        
        .security-info {
          h4 {
            font-size: 15px;
            font-weight: 600;
            color: var(--text-primary);
            margin-bottom: 6px;
          }
          
          p {
            font-size: 13px;
            color: var(--text-muted);
          }
        }
        
        :deep(.el-button) {
          border-radius: $radius-md;
          font-weight: 500;
        }
        
        :deep(.el-switch) {
          .el-switch__core {
            border-radius: 10px;
          }
          &.is-checked .el-switch__core {
            background: linear-gradient(135deg, var(--primary-color) 0%, var(--primary-hover) 100%);
          }
        }
      }
      
      // ============================================
      // Notification Items
      // ============================================
      .notification-group {
        margin-bottom: 28px;

        .notification-scope-caption {
          margin-bottom: 18px;
          font-size: 13px;
          line-height: 1.6;
          color: var(--text-muted);
        }

        .notification-grid-header,
        .notification-item {
          display: grid;
          grid-template-columns: minmax(0, 1fr) 120px 120px;
          align-items: center;
          gap: 16px;
        }

        .notification-module-tabs {
          display: grid;
          grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
          gap: 12px;
          margin-bottom: 20px;
        }

        .module-tab {
          border: 1px solid var(--border-color-light);
          background: var(--bg-secondary);
          border-radius: 12px;
          padding: 14px 16px;
          text-align: left;
          cursor: pointer;

          &.active {
            border-color: var(--primary-color);
            background: var(--primary-lighter);
          }

          .module-tab-label {
            display: block;
            font-size: 14px;
            font-weight: 600;
            color: var(--text-primary);
            margin-bottom: 4px;
          }

          .module-tab-count {
            font-size: 12px;
            color: var(--text-muted);
          }
        }

        .notification-module-panel {
          border: 1px solid var(--border-color-light);
          border-radius: 16px;
          padding: 20px;
          background: var(--bg-secondary);
        }

        .notification-grid-header {
          padding-bottom: 12px;
          border-bottom: 1px solid var(--border-color-light);
        }

        .notification-family-header {
          margin-bottom: 8px;

          .notification-family-label {
            display: block;
            font-size: 15px;
            font-weight: 600;
            color: var(--text-primary);
            margin-bottom: 4px;
          }

          .notification-family-desc {
            font-size: 12px;
            color: var(--text-muted);
            margin-bottom: 6px;
          }

          .scope-hint {
            display: inline-flex;
            font-size: 12px;
            color: var(--primary-color);
            background: var(--primary-lighter);
            border-radius: 999px;
            padding: 4px 10px;
          }
        }

        .notification-channel {
          text-align: center;
          font-size: 12px;
          font-weight: 500;
          color: var(--text-muted);
        }

        .notification-item {
          padding: 18px 0;
          border-bottom: 1px solid var(--border-color-light);
           
          .notification-info {
            .notification-label {
              display: block;
              font-size: 14px;
              color: var(--text-primary);
              margin-bottom: 4px;
              font-weight: 500;
            }
            
            .notification-desc {
              font-size: 13px;
              color: var(--text-muted);
            }
          }

          .notification-switch {
            display: flex;
            justify-content: center;
          }
           
          :deep(.el-switch) {
            .el-switch__core {
              border-radius: 10px;
            }
            &.is-checked .el-switch__core {
              background: linear-gradient(135deg, var(--primary-color) 0%, var(--primary-hover) 100%);
            }
          }
        }
      }
      
      // ============================================
      // Users Section
      // ============================================
      .users-header {
        display: flex;
        justify-content: space-between;
        margin-bottom: 20px;

        .workspace-name {
          font-weight: 600;
          color: var(--text-primary);
        }

        .workspace-role {
          margin-top: 4px;
          font-size: 12px;
          color: var(--text-muted);
        }
        
        :deep(.el-input__wrapper) {
          background: var(--bg-secondary);
          border-radius: $radius-md;
          box-shadow: $shadow-inset;
          border: 1px solid var(--border-color-light);
        }
        
        :deep(.el-button--primary) {
          border-radius: $radius-md;
          font-weight: 600;
        }
      }

      .empty-hint {
        color: var(--text-muted);
      }

      .invitation-block {
        margin-top: 24px;

        h3 {
          margin-bottom: 12px;
          font-size: 15px;
          color: var(--text-primary);
        }
      }

      .form-hint {
        margin-top: 8px;
        font-size: 12px;
        line-height: 1.5;
        color: var(--text-muted);
      }
      
      :deep(.el-table) {
        background: transparent;
        
        th.el-table__cell {
          background: var(--bg-secondary);
          color: var(--text-secondary);
          font-weight: 600;
          font-size: 13px;
          border-bottom: 1px solid var(--border-color);
        }
        
        td.el-table__cell {
          color: var(--text-primary);
          border-bottom: 1px solid var(--border-color-light);
        }
        
        .el-table__row:hover > td.el-table__cell {
          background: var(--primary-lighter);
        }
        
        .el-tag {
          border-radius: $radius-full;
          padding: 4px 12px;
          font-weight: 500;
          border: none;
        }
      }
      
      // ============================================
      // Integration List
      // ============================================
      .integration-list {
        .integration-item {
          display: flex;
          align-items: center;
          padding: 24px;
          border: 1px solid var(--border-color);
          border-radius: $radius-lg;
          margin-bottom: 16px;
          transition: all $transition-base;
          background: var(--bg-secondary);
          
          &:hover {
            border-color: var(--border-color-hover);
            box-shadow: $shadow-sm;
          }
          
          .integration-icon {
            width: 52px;
            height: 52px;
            display: flex;
            align-items: center;
            justify-content: center;
            border-radius: $radius-md;
            margin-right: 20px;
            box-shadow: $shadow-sm;
            
            &.dingtalk {
              background: linear-gradient(135deg, var(--primary-lighter) 0%, var(--primary-light) 100%);
              color: var(--primary-color);
            }
            
            &.wechat {
              background: linear-gradient(135deg, var(--success-light) 0%, rgba(31, 188, 132, 0.08) 100%);
              color: var(--success-color);
            }
            
            &.ldap {
              background: linear-gradient(135deg, var(--warning-light) 0%, rgba(242, 159, 56, 0.1) 100%);
              color: var(--warning-color);
            }
          }
          
          .integration-info {
            flex: 1;
            
            h4 {
              font-size: 15px;
              font-weight: 600;
              color: var(--text-primary);
              margin-bottom: 6px;
            }
            
            p {
              font-size: 13px;
              color: var(--text-muted);
            }
          }
          
          :deep(.el-button--primary) {
            border-radius: $radius-md;
            font-weight: 500;
            background: linear-gradient(135deg, var(--primary-color) 0%, var(--primary-hover) 100%);
            border: none;
            box-shadow: $shadow-sm;
          }
        }
      }
    }
  }
}
</style>

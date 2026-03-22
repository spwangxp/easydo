<template>
  <div class="settings-container">
    <div class="settings-header">
      <h1 class="page-title">设置</h1>
    </div>
    
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
        
        <!-- 用户管理 -->
        <div v-if="activeMenu === 'users'" class="settings-section">
          <h2 class="section-title">工作空间成员管理</h2>

          <div v-if="!userStore.currentWorkspaceId" class="empty-hint">请先在顶部切换到一个工作空间</div>

          <template v-else>
            <div class="users-header">
              <div>
                <div class="workspace-name">{{ userStore.currentWorkspace?.name }}</div>
                <div class="workspace-role">当前角色：{{ roleText(userStore.currentWorkspace?.role) }}</div>
              </div>
              <div class="member-actions" v-if="canManageMembers || canCreateUsers">
                <el-button v-if="isPlatformAdmin" type="success" plain @click="openCreateWorkspaceDialog">创建工作空间</el-button>
                <el-button v-if="canCreateUsers" type="primary" plain @click="openCreateUserDialog">创建用户</el-button>
                <el-button v-if="canManageMembers" type="primary" @click="inviteDialogVisible = true">邀请成员</el-button>
              </div>
            </div>

            <el-table :data="members" style="width: 100%">
              <el-table-column prop="username" label="用户名" width="150" />
              <el-table-column prop="email" label="邮箱" min-width="220" />
              <el-table-column prop="role" label="角色" width="180">
                <template #default="{ row }">
                  <el-select
                    v-if="canManageMembers"
                    :model-value="row.role"
                    size="small"
                    @change="(value) => handleRoleChange(row, value)"
                  >
                    <el-option label="Viewer" value="viewer" />
                    <el-option label="Developer" value="developer" />
                    <el-option label="Maintainer" value="maintainer" />
                    <el-option label="Owner" value="owner" />
                  </el-select>
                  <el-tag v-else size="small">{{ roleText(row.role) }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="status" label="状态" width="120">
                <template #default="{ row }">
                  <el-tag :type="row.status === 'active' ? 'success' : 'info'" size="small">
                    {{ row.status === 'active' ? '已启用' : '已禁用' }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column label="操作" width="120">
                <template #default="{ row }">
                  <el-button v-if="canManageMembers" type="danger" link size="small" @click="handleRemoveMember(row)">移除</el-button>
                </template>
              </el-table-column>
            </el-table>

            <div class="invitation-block">
              <h3>待处理邀请</h3>
              <el-table :data="invitations" style="width: 100%">
                <el-table-column prop="email" label="邮箱" min-width="220" />
                <el-table-column prop="role" label="角色" width="140">
                  <template #default="{ row }">{{ roleText(row.role) }}</template>
                </el-table-column>
                <el-table-column prop="status" label="状态" width="120" />
                <el-table-column prop="expires_at" label="过期时间" width="180">
                  <template #default="{ row }">{{ formatDateTime(row.expires_at) }}</template>
                </el-table-column>
                <el-table-column label="操作" width="120">
                  <template #default="{ row }">
                    <el-button v-if="canManageMembers && row.status === 'pending'" type="danger" link size="small" @click="handleRevokeInvitation(row)">撤销</el-button>
                  </template>
                </el-table-column>
              </el-table>
            </div>

            <el-dialog v-model="inviteDialogVisible" title="邀请成员" width="460px">
              <el-form :model="inviteForm" label-width="80px">
                <el-form-item label="邮箱">
                  <el-input v-model="inviteForm.email" placeholder="member@example.com" />
                </el-form-item>
                <el-form-item label="角色">
                  <el-select v-model="inviteForm.role" style="width: 100%">
                    <el-option label="Viewer" value="viewer" />
                    <el-option label="Developer" value="developer" />
                    <el-option label="Maintainer" value="maintainer" />
                    <el-option label="Owner" value="owner" />
                  </el-select>
                </el-form-item>
              </el-form>
              <template #footer>
                <el-button @click="inviteDialogVisible = false">取消</el-button>
                <el-button type="primary" :loading="inviteLoading" @click="handleInviteSubmit">发送邀请</el-button>
              </template>
            </el-dialog>

            <el-dialog v-model="inviteResultVisible" title="邀请链接" width="520px">
              <div class="invite-result-text">请复制下面的邀请链接并发送给成员，成员登录后访问该链接即可加入工作空间。</div>
              <el-input :model-value="inviteResultLink" readonly />
              <template #footer>
                <el-button @click="inviteResultVisible = false">关闭</el-button>
                <el-button type="primary" @click="copyInviteLink">复制链接</el-button>
              </template>
            </el-dialog>

            <el-dialog v-model="createUserDialogVisible" title="创建用户" width="520px">
              <el-form :model="createUserForm" label-width="100px">
                <el-form-item label="用户名">
                  <el-input v-model="createUserForm.username" placeholder="请输入用户名" />
                </el-form-item>
                <el-form-item label="初始密码">
                  <el-input v-model="createUserForm.password" type="password" show-password placeholder="请输入初始密码" />
                </el-form-item>
                <el-form-item label="邮箱">
                  <el-input v-model="createUserForm.email" placeholder="请输入邮箱" />
                </el-form-item>
                <el-form-item label="昵称">
                  <el-input v-model="createUserForm.nickname" placeholder="请输入昵称" />
                </el-form-item>
                <el-form-item v-if="isPlatformAdmin" label="平台角色">
                  <el-select v-model="createUserForm.system_role" style="width: 100%">
                    <el-option label="User" value="user" />
                    <el-option label="Admin" value="admin" />
                  </el-select>
                </el-form-item>
                <el-form-item v-if="isPlatformAdmin" label="额外绑定">
                  <el-switch v-model="createUserForm.bind_workspace" active-text="绑定其他工作空间" inactive-text="仅创建个人工作空间" />
                </el-form-item>
                <el-form-item v-if="isPlatformAdmin" label="绑定工作空间">
                  <el-select v-model="createUserForm.workspace_id" :disabled="!createUserForm.bind_workspace" style="width: 100%" filterable clearable placeholder="可选：选择额外绑定的工作空间">
                    <el-option v-for="workspace in workspaceOptions" :key="workspace.id" :label="workspace.name" :value="workspace.id" />
                  </el-select>
                  <div class="form-hint">不选择时，仅创建用户及其个人工作空间，不会自动加入当前工作空间。</div>
                </el-form-item>
                <el-form-item v-else label="绑定工作空间">
                  <el-input :model-value="userStore.currentWorkspace?.name || '-'" disabled />
                </el-form-item>
                <el-form-item v-if="!isPlatformAdmin || createUserForm.bind_workspace" label="工作空间角色">
                  <el-select v-model="createUserForm.workspace_role" style="width: 100%">
                    <el-option v-for="role in createUserRoleOptions" :key="role.value" :label="role.label" :value="role.value" />
                  </el-select>
                </el-form-item>
              </el-form>
              <template #footer>
                <el-button @click="createUserDialogVisible = false">取消</el-button>
                <el-button type="primary" :loading="createUserLoading" @click="handleCreateUserSubmit">创建</el-button>
              </template>
            </el-dialog>

            <el-dialog v-model="createWorkspaceDialogVisible" title="创建工作空间" width="520px">
              <el-form :model="createWorkspaceForm" label-width="100px">
                <el-form-item label="名称">
                  <el-input v-model="createWorkspaceForm.name" placeholder="请输入工作空间名称" />
                </el-form-item>
                <el-form-item label="标识">
                  <el-input v-model="createWorkspaceForm.slug" placeholder="可选：用于生成工作空间标识" />
                </el-form-item>
                <el-form-item label="描述">
                  <el-input v-model="createWorkspaceForm.description" type="textarea" :rows="3" placeholder="可选：描述该工作空间用途" />
                </el-form-item>
              </el-form>
              <div class="form-hint">创建后仅会将当前管理员加入该工作空间并设为 Owner，不会自动创建其他成员、项目、流水线或私有执行器。</div>
              <template #footer>
                <el-button @click="createWorkspaceDialogVisible = false">取消</el-button>
                <el-button type="primary" :loading="createWorkspaceLoading" @click="handleCreateWorkspaceSubmit">创建</el-button>
              </template>
            </el-dialog>
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
import { ElMessage, ElMessageBox } from 'element-plus'
import { useUserStore } from '@/stores/user'
import {
  NOTIFICATION_CHANNELS,
  NOTIFICATION_EVENT_GROUPS,
  listNotificationPreferences,
  resolveNotificationPreferenceEnabled,
  upsertNotificationPreference,
  upsertNotificationPreferenceInList
} from '@/api/notification'
import { createWorkspace, createWorkspaceInvitation, getWorkspaceInvitations, getWorkspaceList, getWorkspaceMembers, removeWorkspaceMember, revokeWorkspaceInvitation, updateWorkspaceMember } from '@/api/workspace'
import { createUser } from '@/api/user'
import { 
  Setting, 
  Lock, 
  Bell, 
  User, 
  Link,
  Upload,
  ChatDotRound,
  ChatLineRound,
  Key,
  Search
} from '@element-plus/icons-vue'

const userStore = useUserStore()
const activeMenu = ref('basic')
const members = ref([])
const invitations = ref([])
const inviteDialogVisible = ref(false)
const inviteLoading = ref(false)
const inviteResultVisible = ref(false)
const inviteResultLink = ref('')
const createUserDialogVisible = ref(false)
const createUserLoading = ref(false)
const createWorkspaceDialogVisible = ref(false)
const createWorkspaceLoading = ref(false)
const workspaceOptions = ref([])
const notificationEventGroups = computed(() => {
  return NOTIFICATION_EVENT_GROUPS.map(group => ({
    ...group,
    events: group.events.map(event => ({ ...event, family: group.value }))
  }))
})
const notificationChannels = NOTIFICATION_CHANNELS
const notificationPreferences = ref([])
const notificationPreferencesLoading = ref(false)
const notificationSavingKeys = ref([])
const activeNotificationModule = ref(NOTIFICATION_EVENT_GROUPS[0]?.value || '')
const inviteForm = reactive({
  email: '',
  role: 'viewer'
})
const createUserForm = reactive({
  username: '',
  password: '',
  email: '',
  nickname: '',
  system_role: 'user',
  bind_workspace: false,
  workspace_id: undefined,
  workspace_role: 'viewer'
})
const createWorkspaceForm = reactive({
  name: '',
  slug: '',
  description: ''
})

const menuItems = [
  { key: 'basic', name: '基本设置', icon: Setting },
  { key: 'security', name: '安全设置', icon: Lock },
  { key: 'notifications', name: '通知设置', icon: Bell },
  { key: 'users', name: '用户管理', icon: User },
  { key: 'integrations', name: '第三方集成', icon: Link }
]

const settings = reactive({
  systemName: 'EasyDo',
  theme: 'light',
  twoFactorEnabled: false
})

const showPasswordDialog = ref(false)
const showDevicesDialog = ref(false)
const canManageMembers = computed(() => userStore.hasPermission('workspace.member.manage'))
const isPlatformAdmin = computed(() => userStore.userInfo?.role === 'admin')
const canCreateUsers = computed(() => isPlatformAdmin.value || canManageMembers.value)
const createUserRoleOptions = computed(() => {
  if (isPlatformAdmin.value || userStore.currentWorkspace?.role === 'owner') {
    return [
      { label: 'Viewer', value: 'viewer' },
      { label: 'Developer', value: 'developer' },
      { label: 'Maintainer', value: 'maintainer' },
      { label: 'Owner', value: 'owner' }
    ]
  }
  return [
    { label: 'Viewer', value: 'viewer' },
    { label: 'Developer', value: 'developer' }
  ]
})

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

const loadWorkspaceManagementData = async () => {
  if (!userStore.currentWorkspaceId) {
    members.value = []
    invitations.value = []
    return
  }
  try {
    const requests = [getWorkspaceMembers(userStore.currentWorkspaceId)]
    if (canManageMembers.value) {
      requests.push(getWorkspaceInvitations(userStore.currentWorkspaceId))
    }
    const [memberRes, invitationRes] = await Promise.all(requests)
    const memberList = memberRes?.data?.list || []
    members.value = isPlatformAdmin.value
      ? memberList
      : memberList.filter(member => String(member.system_role || '').toLowerCase() !== 'admin')
    invitations.value = invitationRes?.data?.list || []
  } catch (error) {
    ElMessage.error('加载工作空间管理数据失败')
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

const handleInviteSubmit = async () => {
  if (!inviteForm.email) {
    ElMessage.warning('请输入邮箱')
    return
  }
  inviteLoading.value = true
  try {
    const res = await createWorkspaceInvitation(userStore.currentWorkspaceId, inviteForm)
    if (res.code === 200) {
    inviteResultLink.value = `${window.location.origin}/workspace-invitations/${res.data.id}`
      inviteResultVisible.value = true
      ElMessage.success('邀请已生成')
      inviteDialogVisible.value = false
      inviteForm.email = ''
      inviteForm.role = 'viewer'
      await loadWorkspaceManagementData()
    } else {
      ElMessage.error(res.message || '邀请失败')
    }
  } catch (error) {
    ElMessage.error('邀请失败')
  } finally {
    inviteLoading.value = false
  }
}

const copyInviteLink = async () => {
  try {
    await navigator.clipboard.writeText(inviteResultLink.value)
    ElMessage.success('邀请链接已复制')
  } catch (error) {
    ElMessage.error('复制失败，请手动复制链接')
  }
}

const resetCreateUserForm = () => {
  createUserForm.username = ''
  createUserForm.password = ''
  createUserForm.email = ''
  createUserForm.nickname = ''
  createUserForm.system_role = 'user'
  createUserForm.bind_workspace = false
  createUserForm.workspace_id = isPlatformAdmin.value ? undefined : userStore.currentWorkspaceId
  createUserForm.workspace_role = createUserRoleOptions.value[0]?.value || 'viewer'
}

const resetCreateWorkspaceForm = () => {
  createWorkspaceForm.name = ''
  createWorkspaceForm.slug = ''
  createWorkspaceForm.description = ''
}

const openCreateUserDialog = async () => {
  resetCreateUserForm()
  if (isPlatformAdmin.value) {
    try {
      const res = await getWorkspaceList()
      workspaceOptions.value = res?.data?.list || []
    } catch (error) {
      workspaceOptions.value = []
      ElMessage.error('加载工作空间列表失败')
      return
    }
  } else {
    workspaceOptions.value = []
  }
  createUserDialogVisible.value = true
}

const openCreateWorkspaceDialog = () => {
  resetCreateWorkspaceForm()
  createWorkspaceDialogVisible.value = true
}

const handleCreateUserSubmit = async () => {
  if (!createUserForm.username || !createUserForm.password) {
    ElMessage.warning('请输入用户名和初始密码')
    return
  }
  if (isPlatformAdmin.value && createUserForm.bind_workspace && !createUserForm.workspace_id) {
    ElMessage.warning('请选择额外绑定的工作空间，或关闭额外绑定')
    return
  }
  createUserLoading.value = true
  try {
    const payload = {
      username: createUserForm.username,
      password: createUserForm.password,
      email: createUserForm.email,
      nickname: createUserForm.nickname
    }
    if (isPlatformAdmin.value) {
      payload.system_role = createUserForm.system_role
      if (createUserForm.bind_workspace && createUserForm.workspace_id) {
        payload.workspace_id = createUserForm.workspace_id
        payload.workspace_role = createUserForm.workspace_role
      }
    } else {
      payload.workspace_id = userStore.currentWorkspaceId
      payload.workspace_role = createUserForm.workspace_role
    }
    const res = await createUser(payload)
    if (res.code === 200) {
      ElMessage.success('用户创建成功')
      createUserDialogVisible.value = false
      if (Number(payload.workspace_id) === Number(userStore.currentWorkspaceId)) {
        await loadWorkspaceManagementData()
      }
    } else {
      ElMessage.error(res.message || '创建用户失败')
    }
  } catch (error) {
    ElMessage.error(error.response?.data?.message || '创建用户失败')
  } finally {
    createUserLoading.value = false
  }
}

const handleCreateWorkspaceSubmit = async () => {
  if (!createWorkspaceForm.name) {
    ElMessage.warning('请输入工作空间名称')
    return
  }
  createWorkspaceLoading.value = true
  try {
    const res = await createWorkspace({
      name: createWorkspaceForm.name,
      slug: createWorkspaceForm.slug,
      description: createWorkspaceForm.description
    })
    if (res.code === 200) {
      ElMessage.success('工作空间创建成功')
      createWorkspaceDialogVisible.value = false
      await userStore.getUserInfoAction()
      if (isPlatformAdmin.value) {
        const workspaceRes = await getWorkspaceList()
        workspaceOptions.value = workspaceRes?.data?.list || []
      }
    } else {
      ElMessage.error(res.message || '创建工作空间失败')
    }
  } catch (error) {
    ElMessage.error(error.response?.data?.message || '创建工作空间失败')
  } finally {
    createWorkspaceLoading.value = false
  }
}

const handleRoleChange = async (row, role) => {
  try {
    const res = await updateWorkspaceMember(userStore.currentWorkspaceId, row.id, { role })
    if (res.code === 200) {
      ElMessage.success('角色已更新')
      await loadWorkspaceManagementData()
      await userStore.getUserInfoAction()
    } else {
      ElMessage.error(res.message || '更新失败')
    }
  } catch (error) {
    ElMessage.error('更新失败')
  }
}

const handleRemoveMember = async (row) => {
  try {
    await ElMessageBox.confirm(`确认移除成员 ${row.username} 吗？`, '移除成员', { type: 'warning' })
    const res = await removeWorkspaceMember(userStore.currentWorkspaceId, row.id)
    if (res.code === 200) {
      ElMessage.success('成员已移除')
      await loadWorkspaceManagementData()
    } else {
      ElMessage.error(res.message || '移除失败')
    }
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error('移除失败')
    }
  }
}

const handleRevokeInvitation = async (row) => {
  try {
    await ElMessageBox.confirm(`确认撤销发往 ${row.email} 的邀请吗？`, '撤销邀请', { type: 'warning' })
    const res = await revokeWorkspaceInvitation(userStore.currentWorkspaceId, row.id)
    if (res.code === 200) {
      ElMessage.success('邀请已撤销')
      await loadWorkspaceManagementData()
    } else {
      ElMessage.error(res.message || '撤销失败')
    }
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error('撤销失败')
    }
  }
}

watch(() => [activeMenu.value, userStore.currentWorkspaceId], async ([menu]) => {
  if (menu === 'users') {
    await loadWorkspaceManagementData()
  }
  if (menu === 'notifications') {
    await loadNotificationPreferences()
  }
}, { immediate: true })
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.settings-container {
  .settings-header {
    margin-bottom: 28px;
    
    .page-title {
      font-family: $font-family-display;
      font-size: 28px;
      font-weight: 700;
      color: var(--text-primary);
      letter-spacing: -0.02em;
    }
  }
  
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

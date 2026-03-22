<template>
  <div class="profile-container">
    <div class="profile-header">
      <h1 class="page-title">个人中心</h1>
      <div class="page-subtitle">当前工作空间：{{ userStore.currentWorkspace?.name || '-' }}</div>
    </div>
    
    <div class="profile-layout">
      <aside class="profile-sidebar">
        <div class="user-card">
          <el-avatar :size="80" :src="userInfo.avatar">
            {{ userInfo.username?.charAt(0)?.toUpperCase() }}
          </el-avatar>
          <h3 class="username">{{ userInfo.username }}</h3>
          <p class="email">{{ userInfo.email }}</p>
        </div>
        
        <div class="profile-menu">
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
        </div>
      </aside>
      
      <main class="profile-content">
        <!-- 基本资料 -->
        <div v-if="activeMenu === 'profile'" class="profile-section">
          <h2 class="section-title">基本资料</h2>
          
          <el-form :model="profileForm" label-width="100px" class="profile-form">
            <el-form-item label="用户名">
              <el-input v-model="profileForm.username" disabled />
            </el-form-item>
            
            <el-form-item label="邮箱">
              <el-input v-model="profileForm.email" />
            </el-form-item>
            
            <el-form-item label="手机号">
              <el-input v-model="profileForm.phone" />
            </el-form-item>
            
            <el-form-item label="个人简介">
              <el-input 
                v-model="profileForm.bio" 
                type="textarea" 
                :rows="3"
                placeholder="请输入个人简介"
              />
            </el-form-item>
            
            <el-form-item>
              <el-button type="primary" @click="saveProfile">保存修改</el-button>
            </el-form-item>
          </el-form>
        </div>
        
        <!-- 安全设置 -->
        <div v-if="activeMenu === 'security'" class="profile-section">
          <h2 class="section-title">安全设置</h2>
          
          <div class="security-list">
            <div class="security-item">
              <div class="security-icon">
                <el-icon :size="24"><Lock /></el-icon>
              </div>
              <div class="security-content">
                <h4>登录密码</h4>
                <p>定期修改密码可以提高账户安全性</p>
              </div>
              <el-button @click="showPasswordDialog = true">修改密码</el-button>
            </div>
            
            <div class="security-item">
              <div class="security-icon">
                <el-icon :size="24"><Key /></el-icon>
              </div>
              <div class="security-content">
                <h4>API Token</h4>
                <p>用于API访问的认证Token</p>
              </div>
              <el-button @click="regenerateToken">重新生成</el-button>
            </div>
          </div>
        </div>
        
        <!-- 偏好设置 -->
        <div v-if="activeMenu === 'preferences'" class="profile-section">
          <h2 class="section-title">个人通知偏好</h2>
          
          <div v-loading="preferencesLoading" class="preferences-form">
            <div class="preferences-tip">以下设置会作为你的默认通知方式，当前工作空间中的通知设置可以覆盖这里的默认值。</div>

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

            <div v-if="currentNotificationGroup" class="preference-module-panel">
              <div class="preference-module-header">
                <h3>{{ currentNotificationGroup.label }}</h3>
                <p>{{ currentNotificationGroup.description }}</p>
                <span v-if="currentNotificationGroup.supports_resource_scope" class="scope-hint">{{ currentNotificationGroup.resource_scope_label }}</span>
              </div>

              <div class="preference-grid-header">
                <span></span>
                <span
                  v-for="channel in notificationChannels"
                  :key="channel.value"
                  class="preference-channel"
                >
                  {{ channel.label }}
                </span>
              </div>

              <div
                v-for="event in currentNotificationGroup.events"
                :key="event.value"
                class="preference-row"
              >
                <div class="form-label">
                  <h4>{{ event.label }}</h4>
                  <p>{{ event.description }}</p>
                </div>

                <div class="preference-switch">
                  <el-switch
                    :model-value="getGlobalPreferenceEnabled(event.value, 'in_app')"
                    :loading="isPreferenceSaving(null, event.value, 'in_app')"
                    @change="(value) => updateGlobalPreference(event, 'in_app', value)"
                  />
                </div>

                <div class="preference-switch">
                  <el-switch
                    :model-value="getGlobalPreferenceEnabled(event.value, 'email')"
                    :loading="isPreferenceSaving(null, event.value, 'email')"
                    @change="(value) => updateGlobalPreference(event, 'email', value)"
                  />
                </div>
              </div>
            </div>
          </div>
        </div>
        
        <!-- 登录设备 -->
        <div v-if="activeMenu === 'devices'" class="profile-section">
          <h2 class="section-title">登录设备管理</h2>
          
          <div class="devices-list">
            <div 
              v-for="device in devices" 
              :key="device.id"
              class="device-item"
              :class="{ current: device.current }"
            >
              <div class="device-icon">
                <el-icon :size="32"><Monitor /></el-icon>
              </div>
              <div class="device-content">
                <div class="device-header">
                  <span class="device-name">{{ device.browser }} on {{ device.os }}</span>
                  <el-tag v-if="device.current" type="success" size="small">当前设备</el-tag>
                </div>
                <p class="device-info">{{ device.ip }} · {{ device.location }}</p>
                <p class="device-time">最后活跃: {{ device.lastActive }}</p>
              </div>
              <el-button 
                v-if="!device.current" 
                type="danger" 
                size="small"
                @click="logoutDevice(device.id)"
              >
                退出登录
              </el-button>
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
import { 
  User, 
  Lock, 
  Setting, 
  Monitor,
  Key
} from '@element-plus/icons-vue'

const activeMenu = ref('profile')
const userStore = useUserStore()

const menuItems = [
  { key: 'profile', name: '基本资料', icon: User },
  { key: 'security', name: '安全设置', icon: Lock },
  { key: 'preferences', name: '偏好设置', icon: Setting },
  { key: 'devices', name: '登录设备', icon: Monitor }
]

const userInfo = computed(() => ({
  username: userStore.userInfo?.username || '-',
  email: userStore.userInfo?.email || '-',
  avatar: userStore.userInfo?.avatar || ''
}))

const profileForm = reactive({
  username: '',
  email: '',
  phone: '',
  bio: ''
})

watch(() => userStore.userInfo, (value) => {
  profileForm.username = value?.username || ''
  profileForm.email = value?.email || ''
}, { immediate: true, deep: true })

const notificationEventGroups = computed(() => {
  return NOTIFICATION_EVENT_GROUPS.map(group => ({
    ...group,
    events: group.events.map(event => ({ ...event, family: group.value }))
  }))
})
const notificationChannels = NOTIFICATION_CHANNELS
const preferencesLoading = ref(false)
const notificationPreferences = ref([])
const savingPreferenceKeys = ref([])
const activeNotificationModule = ref(NOTIFICATION_EVENT_GROUPS[0]?.value || '')

const devices = ref([
  {
    id: 1,
    browser: 'Chrome 144',
    os: 'Mac OS X',
    ip: '192.168.1.100',
    location: 'Shanghai, China',
    lastActive: '刚刚',
    current: true
  },
  {
    id: 2,
    browser: 'Edge 143',
    os: 'Windows 11',
    ip: '192.168.1.101',
    location: 'Shanghai, China',
    lastActive: '2 小时前',
    current: false
  }
])

const showPasswordDialog = ref(false)

const saveProfile = () => {
  console.log('保存资料')
}

const regenerateToken = () => {
  console.log('重新生成Token')
}

const logoutDevice = (deviceId) => {
  devices.value = devices.value.filter(d => d.id !== deviceId)
}

const globalNotificationPreferences = computed(() => {
  return notificationPreferences.value.filter(item => item.workspace_id === null || item.workspace_id === undefined)
})

const getPreferenceSavingKey = (workspaceId, eventType, channel) => {
  return `${workspaceId ?? 'global'}:${eventType}:${channel}`
}

const setPreferenceSaving = (savingKey, isSaving) => {
  if (isSaving) {
    if (!savingPreferenceKeys.value.includes(savingKey)) {
      savingPreferenceKeys.value = [...savingPreferenceKeys.value, savingKey]
    }
    return
  }
  savingPreferenceKeys.value = savingPreferenceKeys.value.filter(item => item !== savingKey)
}

const isPreferenceSaving = (workspaceId, eventType, channel) => {
  return savingPreferenceKeys.value.includes(getPreferenceSavingKey(workspaceId, eventType, channel))
}

const currentNotificationGroup = computed(() => {
  return notificationEventGroups.value.find(group => group.value === activeNotificationModule.value) || notificationEventGroups.value[0] || null
})

const getGlobalPreferenceEnabled = (eventType, channel) => {
  return resolveNotificationPreferenceEnabled(globalNotificationPreferences.value, {
    eventType,
    channel,
    workspaceId: null,
    defaultEnabled: true
  })
}

const loadNotificationPreferences = async () => {
  preferencesLoading.value = true
  try {
    const res = await listNotificationPreferences()
    if (res.code === 200) {
      notificationPreferences.value = res.data?.list || []
      return
    }
    ElMessage.error(res.message || '加载通知偏好失败')
  } catch (error) {
    ElMessage.error('加载通知偏好失败')
  } finally {
    preferencesLoading.value = false
  }
}

const updateGlobalPreference = async (event, channel, enabled) => {
  const savingKey = getPreferenceSavingKey(null, event.value, channel)
  setPreferenceSaving(savingKey, true)
  try {
    const res = await upsertNotificationPreference({
      workspace_id: null,
      family: event.family,
      event_type: event.value,
      channel,
      enabled
    })
    if (res.code === 200) {
      notificationPreferences.value = upsertNotificationPreferenceInList(notificationPreferences.value, res.data)
      ElMessage.success('个人通知偏好已更新')
      return
    }
    ElMessage.error(res.message || '更新通知偏好失败')
  } catch (error) {
    ElMessage.error('更新通知偏好失败')
  } finally {
    setPreferenceSaving(savingKey, false)
  }
}

watch(() => userStore.currentWorkspaceId, async () => {
  await loadNotificationPreferences()
}, { immediate: true })
</script>

<style lang="scss" scoped>
.profile-container {
  .profile-header {
    margin-bottom: 20px;
    
    .page-title {
      font-size: 24px;
      font-weight: 600;
      color: var(--text-primary);
    }
  }
  
  .profile-layout {
    display: flex;
    gap: 20px;
    
    .profile-sidebar {
      width: 280px;
      flex-shrink: 0;
      
      .user-card {
        background: var(--bg-card);
        border-radius: 8px;
        padding: 30px 20px;
        text-align: center;
        margin-bottom: 16px;
        
        .el-avatar {
          background: #409EFF;
          font-size: 28px;
          margin-bottom: 16px;
        }
        
        .username {
          font-size: 18px;
          font-weight: 500;
          color: var(--text-primary);
          margin-bottom: 8px;
        }
        
        .email {
          font-size: 13px;
          color: var(--text-muted);
        }
      }
      
      .profile-menu {
        background: var(--bg-card);
        border-radius: 8px;
        padding: 12px;
        
        .menu-item {
          display: flex;
          align-items: center;
          gap: 12px;
          padding: 12px 16px;
          color: var(--text-secondary);
          cursor: pointer;
          border-radius: 6px;
          transition: all 0.3s;
          
          &:hover {
            background: var(--bg-secondary);
          }
          
          &.active {
            color: var(--primary-color);
            background: var(--primary-lighter);
          }
        }
      }
    }
    
    .profile-content {
      flex: 1;
      background: var(--bg-card);
      border-radius: 8px;
      padding: 24px;
      
      .profile-section {
        .section-title {
          font-size: 18px;
          font-weight: 500;
          color: var(--text-primary);
          margin-bottom: 24px;
          padding-bottom: 16px;
          border-bottom: 1px solid #ebeef5;
        }
      }
      
      .profile-form {
        max-width: 500px;
      }
      
      .security-list {
        .security-item {
          display: flex;
          align-items: center;
          padding: 20px;
          border: 1px solid #ebeef5;
          border-radius: 8px;
          margin-bottom: 12px;
          
          .security-icon {
            width: 48px;
            height: 48px;
            display: flex;
            align-items: center;
            justify-content: center;
            background: var(--primary-lighter);
            color: var(--primary-color);
            border-radius: 8px;
            margin-right: 16px;
          }
          
          .security-content {
            flex: 1;
            
            h4 {
              font-size: 14px;
              font-weight: 500;
              color: var(--text-primary);
              margin-bottom: 4px;
            }
            
            p {
              font-size: 12px;
              color: var(--text-muted);
            }
          }
        }
      }
      
      .preferences-form {
        .preferences-tip {
          margin-bottom: 16px;
          font-size: 13px;
          line-height: 1.6;
          color: var(--text-muted);
        }

        .preference-grid-header,
        .preference-row {
          display: grid;
          grid-template-columns: minmax(0, 1fr) 120px 120px;
          gap: 16px;
          align-items: center;
        }

        .preference-grid-header {
          padding-bottom: 12px;
          border-bottom: 1px solid var(--border-color-light);
        }

        .preference-channel {
          text-align: center;
          font-size: 12px;
          font-weight: 500;
          color: var(--text-muted);
        }

        .preference-row {
          padding: 20px 0;
          border-bottom: 1px solid var(--border-color-light);

          .form-label {
            h4 {
              font-size: 14px;
              font-weight: 500;
              color: var(--text-primary);
              margin-bottom: 4px;
            }
            
            p {
              font-size: 12px;
              color: var(--text-muted);
            }
          }
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

        .preference-module-panel {
          border: 1px solid var(--border-color-light);
          border-radius: 16px;
          padding: 20px;
          background: var(--bg-secondary);
        }

        .preference-module-header {
          margin-bottom: 12px;

          h3 {
            font-size: 15px;
            font-weight: 600;
            color: var(--text-primary);
            margin-bottom: 4px;
          }

          p {
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

        .preference-switch {
          display: flex;
          justify-content: center;
        }
      }
      
      .devices-list {
        .device-item {
          display: flex;
          align-items: center;
          padding: 20px;
          border: 1px solid #ebeef5;
          border-radius: 8px;
          margin-bottom: 12px;
          
          &.current {
            background: var(--success-light);
            border-color: var(--success-color);
          }
          
          .device-icon {
            width: 56px;
            height: 56px;
            display: flex;
            align-items: center;
            justify-content: center;
            background: var(--bg-secondary);
            border-radius: 8px;
            margin-right: 16px;
            color: var(--text-secondary);
          }
          
          .device-content {
            flex: 1;
            
            .device-header {
              display: flex;
              align-items: center;
              gap: 12px;
              margin-bottom: 4px;
              
              .device-name {
                font-size: 14px;
                font-weight: 500;
                color: var(--text-primary);
              }
            }
            
            .device-info,
            .device-time {
              font-size: 12px;
              color: var(--text-muted);
              margin-top: 4px;
            }
          }
        }
      }
    }
  }
}
</style>

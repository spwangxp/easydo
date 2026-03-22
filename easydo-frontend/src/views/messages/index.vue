<template>
  <div class="messages-container">
    <div class="messages-header">
      <div>
        <h1 class="page-title">消息</h1>
        <div class="page-subtitle">当前工作空间：{{ userStore.currentWorkspace?.name || '-' }}</div>
      </div>
      <el-button type="text" :disabled="notificationStore.unreadCount === 0 || loading" @click="handleMarkAllRead">全部已读</el-button>
    </div>
    
    <div class="messages-layout">
      <aside class="messages-sidebar">
        <div 
          v-for="item in tabs" 
          :key="item.key"
          class="tab-item"
          :class="{ active: activeTab === item.key }"
          @click="activeTab = item.key"
        >
          <el-icon><component :is="item.icon" /></el-icon>
          <span>{{ item.name }}</span>
          <el-badge v-if="item.count > 0" :value="item.count" class="tab-badge" />
        </div>
      </aside>
      
      <main v-loading="loading" class="messages-list">
        <div v-if="filteredMessages.length === 0" class="empty-state">
          <el-icon :size="64"><Bell /></el-icon>
          <p>暂无消息</p>
        </div>
        
        <div 
          v-for="msg in filteredMessages" 
          :key="msg.id"
          class="message-item"
          :class="{ unread: !msg.is_read }"
          @click="handleRead(msg)"
        >
          <div class="message-icon" :class="getMessageFamilyMeta(msg.family).className">
            <el-icon :size="20"><component :is="getMessageFamilyMeta(msg.family).icon" /></el-icon>
          </div>
          
          <div class="message-content">
            <div class="message-header">
              <div class="message-heading">
                <span class="message-title">{{ msg.title }}</span>
                <span class="message-family">{{ getMessageFamilyMeta(msg.family).label }}</span>
              </div>
              <span class="message-time">{{ formatTime(msg.created_at) }}</span>
            </div>
            <p class="message-body">{{ msg.content }}</p>
          </div>
        </div>
      </main>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import { 
  Bell, 
  CircleCheck, 
  Warning, 
  InfoFilled,
  Connection,
  Promotion,
  User,
  Monitor
} from '@element-plus/icons-vue'
import { getNotificationInbox, markAllNotificationsRead, markNotificationRead } from '@/api/notification'
import { useNotificationStore } from '@/stores/notification'
import { useUserStore } from '@/stores/user'

const userStore = useUserStore()
const notificationStore = useNotificationStore()
const activeTab = ref('all')
const loading = ref(false)
const allMessages = ref([])
const unreadMessages = ref([])
const totalCount = ref(0)

const inboxQuery = {
  page: 1,
  page_size: 100
}

let inboxPollTimer = null

const stopInboxPolling = () => {
  if (inboxPollTimer) {
    clearInterval(inboxPollTimer)
    inboxPollTimer = null
  }
}

const startInboxPolling = () => {
  stopInboxPolling()
  inboxPollTimer = setInterval(() => {
    if (document.hidden) {
      return
    }
    loadInboxData()
  }, 15000)
}

const messageFamilyMetaMap = {
  'pipeline.run': {
    label: '流水线运行',
    icon: Connection,
    className: 'pipeline'
  },
  'deployment.request': {
    label: '发布申请',
    icon: Promotion,
    className: 'deployment'
  },
  'agent.lifecycle': {
    label: '执行器状态',
    icon: Monitor,
    className: 'agent'
  },
  'workspace.member': {
    label: '成员变更',
    icon: User,
    className: 'workspace'
  },
  'workspace.invitation': {
    label: '工作空间邀请',
    icon: InfoFilled,
    className: 'workspace'
  }
}

const tabs = computed(() => [
  { key: 'all', name: '全部消息', icon: Bell, count: totalCount.value },
  { key: 'unread', name: '未读消息', icon: Warning, count: notificationStore.unreadCount },
  { key: 'read', name: '已读消息', icon: CircleCheck, count: Math.max(0, totalCount.value - notificationStore.unreadCount) }
])

const filteredMessages = computed(() => {
  if (activeTab.value === 'unread') {
    return unreadMessages.value
  }
  if (activeTab.value === 'read') {
    return allMessages.value.filter(message => message.is_read)
  }
  return allMessages.value
})

const getMessageFamilyMeta = (family) => {
  return messageFamilyMetaMap[family] || {
    label: '通知',
    icon: Bell,
    className: 'general'
  }
}

const parseDateValue = (value) => {
  if (!value) {
    return null
  }
  if (value instanceof Date) {
    return value
  }
  if (typeof value === 'number') {
    return new Date(value > 9999999999 ? value : value * 1000)
  }

  const parsedDate = new Date(value)
  if (!Number.isNaN(parsedDate.getTime())) {
    return parsedDate
  }

  const numericValue = Number(value)
  if (!Number.isNaN(numericValue) && numericValue > 0) {
    return new Date(numericValue > 9999999999 ? numericValue : numericValue * 1000)
  }

  return null
}

const formatTime = (timestamp) => {
  const date = parseDateValue(timestamp)
  if (!date) return '-'
  const now = new Date()
  const diff = now - date
  const hours = Math.floor(diff / (1000 * 60 * 60))
  const minutes = Math.floor((diff % (1000 * 60 * 60)) / (1000 * 60))
  
  if (hours > 24) {
    const year = date.getFullYear()
    const month = String(date.getMonth() + 1).padStart(2, '0')
    const day = String(date.getDate()).padStart(2, '0')
    return `${year}-${month}-${day}`
  } else if (hours > 0) {
    return `${hours} 小时 ${minutes} 分钟前`
  } else if (minutes > 0) {
    return `${minutes} 分钟前`
  }
  return '刚刚'
}

const loadInboxData = async () => {
  loading.value = true
  try {
    const [allRes, unreadRes] = await Promise.all([
      getNotificationInbox(inboxQuery),
      getNotificationInbox({ ...inboxQuery, unread_only: true }),
      notificationStore.refreshUnreadCount()
    ])

    if (allRes.code === 200) {
      allMessages.value = allRes.data?.list || []
      totalCount.value = allRes.data?.total || 0
    }
    if (unreadRes.code === 200) {
      unreadMessages.value = unreadRes.data?.list || []
    }
  } catch (error) {
    console.error('获取消息列表失败:', error)
    ElMessage.error('获取消息失败')
  } finally {
    loading.value = false
  }
}

const handleRead = async (msg) => {
  if (msg.is_read) return
  
  try {
    const res = await markNotificationRead(msg.id)
    if (res.code === 200) {
      await loadInboxData()
      return
    }
    ElMessage.error(res.message || '标记已读失败')
  } catch (error) {
    console.error('标记已读失败:', error)
    ElMessage.error('标记已读失败')
  }
}

const handleMarkAllRead = async () => {
  try {
    const res = await markAllNotificationsRead()
    if (res.code === 200) {
      ElMessage.success('全部已读')
      await loadInboxData()
      return
    }
    ElMessage.error(res.message || '操作失败')
  } catch (error) {
    console.error('全部已读失败:', error)
    ElMessage.error('操作失败')
  }
}

watch(() => userStore.currentWorkspaceId, async () => {
  await loadInboxData()
}, { immediate: true })

onMounted(() => {
  startInboxPolling()
  document.addEventListener('visibilitychange', loadInboxData)
})

onUnmounted(() => {
  stopInboxPolling()
  document.removeEventListener('visibilitychange', loadInboxData)
})
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.messages-container {
  animation: float-up 0.45s ease both;

  .messages-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 20px;

    .page-title {
      font-family: $font-family-display;
      font-size: 32px;
      font-weight: 760;
      letter-spacing: -0.03em;
      color: var(--text-primary);
    }

    :deep(.el-button--text) {
      color: var(--primary-color);
    }
  }

  .messages-layout {
    display: flex;
    gap: 16px;
  }

  .messages-sidebar,
  .messages-list {
    border: 1px solid var(--border-color-light);
    background: var(--bg-card);
    backdrop-filter: $blur-md;
    -webkit-backdrop-filter: $blur-md;
    border-radius: $radius-xl;
    box-shadow: var(--shadow-md);
  }

  .messages-sidebar {
    width: 220px;
    padding: 10px;
    flex-shrink: 0;

    .tab-item {
      display: flex;
      align-items: center;
      gap: 12px;
      padding: 12px 14px;
      border-radius: $radius-md;
      color: var(--text-secondary);
      cursor: pointer;
      transition: all $transition-fast;

      &:hover {
        color: var(--primary-color);
        background: var(--primary-lighter);
      }

      &.active {
        color: var(--primary-color);
        background: var(--primary-lighter);
        box-shadow: inset 0 0 0 1px var(--border-color-hover);
      }

      .tab-badge {
        margin-left: auto;
      }
    }
  }

  .messages-list {
    flex: 1;
    padding: 14px;
    min-height: 420px;

    .empty-state {
      height: 100%;
      min-height: 360px;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      color: var(--text-muted);

      .el-icon {
        margin-bottom: 12px;
      }
    }
  }

  .message-item {
    display: flex;
    gap: 14px;
    padding: 14px;
    border-radius: $radius-lg;
    border: 1px solid transparent;
    cursor: pointer;
    transition: all $transition-fast;
    margin-bottom: 10px;

    &:hover {
      border-color: var(--border-color-hover);
      background: var(--bg-elevated);
      transform: translateY(-1px);
    }

    &.unread {
      background: var(--success-light);
      border-color: var(--border-color-light);

      &:hover {
        background: var(--bg-elevated);
      }
    }
  }

  .message-icon {
    width: 42px;
    height: 42px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border-radius: 12px;
    flex-shrink: 0;
    box-shadow: var(--shadow-sm);

    &.pipeline {
      background: var(--primary-lighter);
      color: var(--primary-color);
    }

    &.deployment {
      background: var(--warning-light);
      color: var(--warning-color);
    }

    &.agent {
      background: var(--danger-light);
      color: var(--danger-color);
    }

    &.workspace {
      background: var(--info-light);
      color: var(--info-color);
    }

    &.general {
      background: var(--success-light);
      color: var(--success-color);
    }
  }

  .message-content {
    flex: 1;
    min-width: 0;
  }

  .message-header {
    display: flex;
    justify-content: space-between;
    gap: 10px;
    margin-bottom: 6px;
  }

  .message-heading {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }

  .message-title {
    font-size: 14px;
    font-weight: 650;
    color: var(--text-primary);
  }

  .message-family {
    font-size: 12px;
    color: var(--text-muted);
  }

  .message-time {
    font-size: 12px;
    color: var(--text-muted);
    white-space: nowrap;
  }

  .message-body {
    font-size: 13px;
    line-height: 1.6;
    color: var(--text-secondary);
  }
}

@media (max-width: 1024px) {
  .messages-container .messages-layout {
    flex-direction: column;
  }

  .messages-container .messages-sidebar {
    width: 100%;
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 8px;
  }
}
</style>

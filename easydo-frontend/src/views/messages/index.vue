<template>
  <div class="messages-container">
    <div class="messages-header">
      <div>
        <h1 class="page-title">消息</h1>
        <div class="page-subtitle">当前工作空间：{{ userStore.currentWorkspace?.name || '-' }}</div>
      </div>
      <el-button type="text" @click="handleMarkAllRead">全部已读</el-button>
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
      
      <main class="messages-list">
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
          <div class="message-icon" :class="msg.type">
            <el-icon v-if="msg.type === 'alert'" :size="20"><Warning /></el-icon>
            <el-icon v-else-if="msg.type === 'system'" :size="20"><InfoFilled /></el-icon>
            <el-icon v-else :size="20"><Bell /></el-icon>
          </div>
          
          <div class="message-content">
            <div class="message-header">
              <span class="message-title">{{ msg.title }}</span>
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
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { 
  Bell, 
  CircleCheck, 
  Warning, 
  CircleClose,
  InfoFilled,
  Connection
} from '@element-plus/icons-vue'
import { getMessageList, getUnreadCount, markAsRead, markAllAsRead } from '@/api/message'
import { useUserStore } from '@/stores/user'

const userStore = useUserStore()
const activeTab = ref('all')
const loading = ref(false)
const messageList = ref([])
const unreadCount = ref(0)

const tabs = computed(() => [
  { key: 'all', name: '全部消息', icon: Bell, count: unreadCount.value },
  { key: 'alert', name: '告警通知', icon: Warning, count: 0 },
  { key: 'system', name: '系统通知', icon: InfoFilled, count: 0 }
])

const filteredMessages = computed(() => {
  if (activeTab.value === 'all') {
    return messageList.value
  } else if (activeTab.value === 'alert') {
    return messageList.value.filter(m => m.type === 'alert')
  } else {
    return messageList.value.filter(m => m.type === 'system')
  }
})

const formatTime = (timestamp) => {
  if (!timestamp) return '-'
  const date = new Date(timestamp * 1000)
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

const fetchMessages = async () => {
  loading.value = true
  try {
    const res = await getMessageList({ page: 1, page_size: 50 })
    if (res.code === 200) {
      messageList.value = res.data.list || []
    }
  } catch (error) {
    console.error('获取消息列表失败:', error)
  } finally {
    loading.value = false
  }
}

const fetchUnreadCount = async () => {
  try {
    const res = await getUnreadCount()
    if (res.code === 200) {
      unreadCount.value = res.data.unread_count || 0
    }
  } catch (error) {
    console.error('获取未读数量失败:', error)
  }
}

const handleRead = async (msg) => {
  if (msg.is_read) return
  
  try {
    await markAsRead(msg.id)
    msg.is_read = true
    unreadCount.value = Math.max(0, unreadCount.value - 1)
  } catch (error) {
    console.error('标记已读失败:', error)
  }
}

const handleMarkAllRead = async () => {
  try {
    await markAllAsRead()
    messageList.value.forEach(m => m.is_read = true)
    unreadCount.value = 0
    ElMessage.success('全部已读')
  } catch (error) {
    console.error('全部已读失败:', error)
    ElMessage.error('操作失败')
  }
}

onMounted(() => {
  fetchMessages()
  fetchUnreadCount()
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

    &.alert {
      background: var(--danger-light);
      color: var(--danger-color);
    }

    &.system {
      background: var(--primary-lighter);
      color: var(--primary-color);
    }

    &:not(.alert):not(.system) {
      background: var(--warning-light);
      color: var(--warning-color);
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

  .message-title {
    font-size: 14px;
    font-weight: 650;
    color: var(--text-primary);
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

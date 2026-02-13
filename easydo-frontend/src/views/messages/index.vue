<template>
  <div class="messages-container">
    <div class="messages-header">
      <h1 class="page-title">消息</h1>
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
.messages-container {
  .messages-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 20px;
    
    .page-title {
      font-size: 24px;
      font-weight: 600;
      color: #303133;
    }
  }
  
  .messages-layout {
    display: flex;
    gap: 20px;
    
    .messages-sidebar {
      width: 200px;
      background: white;
      border-radius: 8px;
      padding: 12px;
      flex-shrink: 0;
      
      .tab-item {
        display: flex;
        align-items: center;
        gap: 12px;
        padding: 12px 16px;
        color: #606266;
        cursor: pointer;
        border-radius: 6px;
        transition: all 0.3s;
        
        &:hover {
          background: #f5f7fa;
        }
        
        &.active {
          color: #409EFF;
          background: #ecf5ff;
        }
        
        .tab-badge {
          margin-left: auto;
        }
      }
    }
    
    .messages-list {
      flex: 1;
      background: white;
      border-radius: 8px;
      padding: 20px;
      
      .empty-state {
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        padding: 60px 20px;
        color: #909399;
        
        .el-icon {
          margin-bottom: 16px;
        }
        
        p {
          font-size: 14px;
        }
      }
      
      .message-item {
        display: flex;
        padding: 20px;
        border-radius: 8px;
        cursor: pointer;
        transition: background 0.3s;
        margin-bottom: 12px;
        
        &:hover {
          background: #f5f7fa;
        }
        
        &.unread {
          background: #f0f9eb;
          
          &:hover {
            background: #e1f3d8;
          }
        }
        
        .message-icon {
          width: 40px;
          height: 40px;
          display: flex;
          align-items: center;
          justify-content: center;
          border-radius: 8px;
          margin-right: 16px;
          flex-shrink: 0;
          
          &.success {
            background: #f0f9eb;
            color: #67C23A;
          }
          
          &.error {
            background: #fef0f0;
            color: #F56C6C;
          }
          
          &.warning {
            background: #fdf6ec;
            color: #E6A23C;
          }
          
          &.info {
            background: #ecf5ff;
            color: #409EFF;
          }
        }
        
        .message-content {
          flex: 1;
          
          .message-header {
            display: flex;
            justify-content: space-between;
            margin-bottom: 8px;
            
            .message-title {
              font-size: 14px;
              font-weight: 500;
              color: #303133;
            }
            
            .message-time {
              font-size: 12px;
              color: #909399;
            }
          }
          
          .message-body {
            font-size: 13px;
            color: #606266;
            margin-bottom: 8px;
            line-height: 1.5;
          }
          
          .message-meta {
            display: flex;
            gap: 16px;
            
            .meta-item {
              display: flex;
              align-items: center;
              gap: 4px;
              font-size: 12px;
              color: #909399;
            }
          }
        }
      }
    }
  }
}
</style>

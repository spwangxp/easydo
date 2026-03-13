<template>
  <el-dialog
    v-model="dialogVisible"
    :title="`日志查看 - ${title}`"
    width="80%"
    top="5vh"
    :close-on-click-modal="false"
  >
    <!-- 日志工具栏 -->
    <div class="log-toolbar">
      <div class="toolbar-left">
        <el-select v-model="logFilter.level" placeholder="日志级别" clearable size="small" style="width: 120px">
          <el-option label="全部" value="" />
          <el-option label="INFO" value="info" />
          <el-option label="WARN" value="warn" />
          <el-option label="ERROR" value="error" />
          <el-option label="DEBUG" value="debug" />
        </el-select>
        <el-input
          v-model="logFilter.keyword"
          placeholder="搜索日志..."
          size="small"
          style="width: 200px; margin-left: 12px"
          clearable
        />
      </div>
      <div class="toolbar-right">
        <el-button-group>
          <el-button size="small" :type="autoScroll ? 'primary' : ''" @click="autoScroll = !autoScroll">
            <el-icon><Bottom /></el-icon>
            自动滚动
          </el-button>
          <el-button size="small" @click="clearLogs">
            <el-icon><Delete /></el-icon>
            清空
          </el-button>
          <el-button size="small" @click="copyLogs">
            <el-icon><CopyDocument /></el-icon>
            复制
          </el-button>
          <el-button size="small" @click="downloadLogs">
            <el-icon><Download /></el-icon>
            下载
          </el-button>
        </el-button-group>
        <el-tag size="small" type="info" style="margin-left: 12px">
          {{ filteredLogs.length }} 条日志
        </el-tag>
      </div>
    </div>
    
    <!-- 日志内容 -->
    <div class="log-container" ref="logContainerRef">
      <div
        v-for="(log, index) in filteredLogs"
        :key="index"
        class="log-line"
        :class="`log-${log.level}`"
      >
        <span class="log-timestamp">{{ formatTimestamp(log.timestamp) }}</span>
        <span class="log-source" v-if="log.source">[{{ log.source }}]</span>
        <span class="log-message">{{ log.message }}</span>
      </div>
      
      <el-empty v-if="filteredLogs.length === 0" description="暂无日志" :image-size="60" />
    </div>
    
    <template #footer>
      <el-button @click="dialogVisible = false">关闭</el-button>
      <el-button type="primary" @click="refreshLogs" :loading="loading">
        <el-icon><Refresh /></el-icon>
        刷新
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed, watch, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import { getRunLogs } from '@/api/pipeline'
import { getTaskLogs } from '@/api/task'
import {
  Bottom,
  Delete,
  CopyDocument,
  Download,
  Refresh
} from '@element-plus/icons-vue'

const props = defineProps({
  taskId: {
    type: Number,
    default: null
  },
  pipelineRunId: {
    type: Number,
    default: null
  },
  pipelineId: {
    type: Number,
    default: null
  },
  title: {
    type: String,
    default: '任务日志'
  }
})

const emit = defineEmits(['refresh'])

const dialogVisible = ref(false)
const loading = ref(false)
const logs = ref([])
const autoScroll = ref(true)
const logContainerRef = ref(null)

const logFilter = ref({
  level: '',
  keyword: ''
})

// 打开对话框
const open = async () => {
  dialogVisible.value = true
  await refreshLogs()
}

// 刷新日志
const refreshLogs = async () => {
  if (!props.taskId && !props.pipelineRunId) return
  
  loading.value = true
  try {
    let response = null
    if (props.taskId) {
      response = await getTaskLogs(props.taskId)
    } else if (props.pipelineId && props.pipelineRunId) {
      response = await getRunLogs(props.pipelineId, props.pipelineRunId)
    }

    if (response?.code === 200) {
      logs.value = response.data.list || []
      await nextTick()
      scrollToBottom()
    }
  } catch (error) {
    console.error('获取日志失败:', error)
    ElMessage.error('获取日志失败')
  } finally {
    loading.value = false
  }
}

// 过滤后的日志
const filteredLogs = computed(() => {
  return logs.value.filter(log => {
    // 按级别过滤
    if (logFilter.value.level && log.level !== logFilter.value.level) {
      return false
    }
    // 按关键词过滤
    if (logFilter.value.keyword) {
      const keyword = logFilter.value.keyword.toLowerCase()
      return log.message.toLowerCase().includes(keyword)
    }
    return true
  })
})

// 格式化时间戳
const formatTimestamp = (timestamp) => {
  if (!timestamp) return '--:--:--'
  return new Date(timestamp * 1000).toLocaleTimeString('zh-CN', {
    hour12: false,
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  })
}

// 滚动到底部
const scrollToBottom = () => {
  if (!autoScroll.value || !logContainerRef.value) return
  logContainerRef.value.scrollTop = logContainerRef.value.scrollHeight
}

// 清空日志
const clearLogs = () => {
  logs.value = []
  ElMessage.success('日志已清空')
}

// 复制日志
const copyLogs = async () => {
  const text = filteredLogs.value.map(log => 
    `[${formatTimestamp(log.timestamp)}] [${log.level.toUpperCase()}] ${log.message}`
  ).join('\n')
  
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('日志已复制到剪贴板')
  } catch (error) {
    ElMessage.error('复制失败')
  }
}

// 下载日志
const downloadLogs = () => {
  const text = filteredLogs.value.map(log => 
    `[${formatTimestamp(log.timestamp)}] [${log.level.toUpperCase()}] ${log.message}`
  ).join('\n')
  
  const blob = new Blob([text], { type: 'text/plain' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `task-logs-${Date.now()}.txt`
  a.click()
  URL.revokeObjectURL(url)
  
  ElMessage.success('日志下载开始')
}

// 监听过滤变化，自动滚动
watch([() => logFilter.value.level, () => logFilter.value.keyword], async () => {
  await nextTick()
  scrollToBottom()
})

// 打开时刷新
watch(dialogVisible, async (visible) => {
  if (visible) {
    await refreshLogs()
  }
})

defineExpose({
  open,
  refreshLogs
})
</script>

<style lang="scss" scoped>
.log-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  background: var(--bg-secondary);
  border-bottom: 1px solid #e4e7ed;
  margin: -20px -20px 0;
  
  .toolbar-left {
    display: flex;
    align-items: center;
  }
  
  .toolbar-right {
    display: flex;
    align-items: center;
  }
}

.log-container {
  max-height: 60vh;
  overflow-y: auto;
  background: #1e1e1e;
  border-radius: 4px;
  padding: 12px;
  font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.8;
  margin-top: 16px;
  
  .log-line {
    display: flex;
    padding: 2px 0;
    
    .log-timestamp {
      color: #858585;
      margin-right: 12px;
      white-space: nowrap;
      flex-shrink: 0;
    }
    
    .log-source {
      color: #6a9955;
      margin-right: 8px;
      white-space: nowrap;
      flex-shrink: 0;
    }
    
    .log-message {
      color: #d4d4d4;
      word-break: break-all;
      white-space: pre-wrap;
    }
    
    &.log-info .log-message {
      color: #d4d4d4;
    }
    
    &.log-warn .log-message,
    &.log-warning .log-message {
      color: #cca700;
    }
    
    &.log-error .log-message {
      color: #f14c4c;
    }
    
    &.log-debug .log-message {
      color: #6a9955;
    }
  }
}
</style>

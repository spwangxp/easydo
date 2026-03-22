import { defineStore } from 'pinia'
import { ref } from 'vue'
import { getNotificationUnreadCount } from '@/api/notification'

export const useNotificationStore = defineStore('notification', () => {
  const unreadCount = ref(0)
  const loading = ref(false)
  let pollTimer = null

  const setUnreadCount = (value) => {
    unreadCount.value = Math.max(0, Number(value) || 0)
  }

  const refreshUnreadCount = async () => {
    loading.value = true
    try {
      const res = await getNotificationUnreadCount()
      if (res.code === 200) {
        setUnreadCount(res.data?.unread_count)
      }
      return unreadCount.value
    } catch (error) {
      console.error('获取未读通知数量失败:', error)
      return unreadCount.value
    } finally {
      loading.value = false
    }
  }

  const reset = () => {
    unreadCount.value = 0
  }

  const stopPolling = () => {
	if (pollTimer) {
	  clearInterval(pollTimer)
	  pollTimer = null
	}
  }

  const startPolling = (intervalMs = 15000) => {
	stopPolling()
	refreshUnreadCount()
	pollTimer = setInterval(() => {
	  if (typeof document !== 'undefined' && document.hidden) {
		return
	  }
	  refreshUnreadCount()
	}, intervalMs)
  }

  return {
    unreadCount,
    loading,
    setUnreadCount,
    refreshUnreadCount,
    reset,
    startPolling,
    stopPolling
  }
})

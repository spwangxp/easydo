import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { login, logout, getUserInfo, refreshAuthToken } from '@/api/user'

const DEFAULT_REFRESH_INTERVAL_SECONDS = 10 * 60
let authInvalidListenerBound = false

export const useUserStore = defineStore('user', () => {
  const token = ref(localStorage.getItem('token') || '')
  const tokenExpiresAt = ref(Number(localStorage.getItem('token_expires_at') || 0))
  const tokenRefreshInterval = ref(Number(localStorage.getItem('token_refresh_interval') || DEFAULT_REFRESH_INTERVAL_SECONDS))
  const userInfo = ref({})
  const permissions = ref([])
  let refreshTimer = null
  let isRefreshing = false

  const isLoggedIn = computed(() => !!token.value)

  function clearRefreshTimer() {
    if (refreshTimer) {
      clearTimeout(refreshTimer)
      refreshTimer = null
    }
  }

  function persistAuthMeta() {
    localStorage.setItem('token_expires_at', String(tokenExpiresAt.value || 0))
    localStorage.setItem('token_refresh_interval', String(tokenRefreshInterval.value || DEFAULT_REFRESH_INTERVAL_SECONDS))
  }

  function clearAuthState() {
    clearRefreshTimer()
    token.value = ''
    tokenExpiresAt.value = 0
    tokenRefreshInterval.value = DEFAULT_REFRESH_INTERVAL_SECONDS
    userInfo.value = {}
    permissions.value = []
    localStorage.removeItem('token')
    localStorage.removeItem('token_expires_at')
    localStorage.removeItem('token_refresh_interval')
  }

  function applyTokenMeta(data = {}) {
    if (typeof data.expires_at === 'number' && data.expires_at > 0) {
      tokenExpiresAt.value = data.expires_at
    }
    if (typeof data.refresh_interval === 'number' && data.refresh_interval > 0) {
      tokenRefreshInterval.value = data.refresh_interval
    }
    persistAuthMeta()
  }

  function scheduleRefresh(delaySeconds) {
    clearRefreshTimer()
    if (!token.value) {
      return
    }

    const waitSeconds = Math.max(30, Number(delaySeconds || tokenRefreshInterval.value || DEFAULT_REFRESH_INTERVAL_SECONDS))
    refreshTimer = setTimeout(async () => {
      if (!token.value) {
        clearRefreshTimer()
        return
      }
      if (isRefreshing) {
        scheduleRefresh(tokenRefreshInterval.value)
        return
      }

      isRefreshing = true
      try {
        const response = await refreshAuthToken()
        if (!response?.data?.token) {
          throw new Error('missing token')
        }
        token.value = response.data.token
        localStorage.setItem('token', token.value)
        applyTokenMeta(response.data)
        scheduleRefresh(tokenRefreshInterval.value)
      } catch (error) {
        clearAuthState()
        if (typeof window !== 'undefined' && window.location.pathname !== '/login') {
          window.location.href = '/login'
        }
      } finally {
        isRefreshing = false
      }
    }, waitSeconds * 1000)
  }

  function startTokenAutoRefresh() {
    if (!token.value) {
      return
    }
    scheduleRefresh(tokenRefreshInterval.value)
  }

  function bindAuthInvalidListener() {
    if (typeof window === 'undefined') {
      return
    }
    if (authInvalidListenerBound) {
      return
    }
    window.addEventListener('easydo-auth-invalid', () => {
      clearAuthState()
    })
    authInvalidListenerBound = true
  }

  async function doLogin(username, password) {
    try {
      const response = await login({ username, password })
      token.value = response.data.token
      localStorage.setItem('token', token.value)
      applyTokenMeta(response.data)
      
      // 获取用户信息
      await getUserInfoAction()
      startTokenAutoRefresh()
      
      return { success: true }
    } catch (error) {
      return { success: false, message: error.response?.data?.message || '登录失败' }
    }
  }

  async function getUserInfoAction() {
    try {
      const response = await getUserInfo()
      userInfo.value = response.data
      permissions.value = response.data.permissions || []
    } catch (error) {
      console.error('获取用户信息失败:', error)
    }
  }

  function setToken(newToken) {
    token.value = newToken
    localStorage.setItem('token', newToken)
  }

  function restoreAuthFromStorage() {
    const storedToken = localStorage.getItem('token')
    if (!storedToken) {
      clearAuthState()
      return
    }

    token.value = storedToken
    tokenExpiresAt.value = Number(localStorage.getItem('token_expires_at') || 0)
    tokenRefreshInterval.value = Number(localStorage.getItem('token_refresh_interval') || DEFAULT_REFRESH_INTERVAL_SECONDS)
    startTokenAutoRefresh()
  }

  function doLogout() {
    return new Promise((resolve) => {
      logout().finally(() => {
        clearAuthState()
        resolve()
      })
    })
  }

  function hasPermission(permission) {
    return permissions.value.includes(permission)
  }

  bindAuthInvalidListener()

  return {
    token,
    tokenExpiresAt,
    tokenRefreshInterval,
    userInfo,
    permissions,
    isLoggedIn,
    doLogin,
    getUserInfoAction,
    setToken,
    restoreAuthFromStorage,
    startTokenAutoRefresh,
    doLogout,
    hasPermission
  }
})

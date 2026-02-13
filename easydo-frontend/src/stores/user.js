import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { login, logout, getUserInfo } from '@/api/user'

export const useUserStore = defineStore('user', () => {
  const token = ref(localStorage.getItem('token') || '')
  const userInfo = ref({})
  const permissions = ref([])

  const isLoggedIn = computed(() => !!token.value)

  async function doLogin(username, password) {
    try {
      const response = await login({ username, password })
      token.value = response.data.token
      localStorage.setItem('token', token.value)
      
      // 获取用户信息
      await getUserInfoAction()
      
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

  function doLogout() {
    return new Promise((resolve) => {
      logout().finally(() => {
        token.value = ''
        userInfo.value = {}
        permissions.value = []
        localStorage.removeItem('token')
        resolve()
      })
    })
  }

  function hasPermission(permission) {
    return permissions.value.includes(permission)
  }

  return {
    token,
    userInfo,
    permissions,
    isLoggedIn,
    doLogin,
    getUserInfoAction,
    setToken,
    doLogout,
    hasPermission
  }
})

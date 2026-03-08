<template>
  <router-view />
</template>

<script setup>
import { onMounted } from 'vue'
import { useUserStore } from '@/stores/user'

const userStore = useUserStore()

// 初始化用户状态
onMounted(() => {
  userStore.restoreAuthFromStorage()
  if (userStore.token) {
    userStore.getUserInfoAction()
  }
})
</script>

<style lang="scss">
#app {
  width: 100%;
  height: 100%;
}
</style>

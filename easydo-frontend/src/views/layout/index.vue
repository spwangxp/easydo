<template>
  <div class="layout-shell">
    <div class="layout-ambience" aria-hidden="true">
      <span class="orb orb-a"></span>
      <span class="orb orb-b"></span>
      <span class="orb orb-c"></span>
    </div>

    <aside class="sidebar" :class="{ collapsed: isCollapsed }">
      <div class="sidebar-header">
        <div class="sidebar-brand">
          <img src="@/assets/images/logo.svg" alt="Logo" class="logo" />
          <div v-show="!isCollapsed" class="brand-text">
            <span class="title">EasyDo</span>
          </div>
        </div>
      </div>

      <nav class="sidebar-nav">
        <div class="nav-section">
          <router-link
            v-for="item in filteredMenuItems"
            :key="item.path"
            :to="item.path"
            class="nav-item"
            :class="{ active: isActive(item.path) }"
          >
            <el-icon class="nav-icon">
              <component :is="item.icon" />
            </el-icon>
            <span v-show="!isCollapsed" class="nav-text">{{ item.name }}</span>
          </router-link>
        </div>
      </nav>

      <div class="sidebar-footer">
        <div class="sidebar-footer-icons">
          <el-badge :value="notificationStore.unreadCount" :max="99" :hidden="notificationStore.unreadCount < 1" class="sidebar-badge">
            <button class="icon-btn sidebar-icon-btn" type="button" @click="router.push('/messages')">
              <el-icon :size="18"><Bell /></el-icon>
            </button>
          </el-badge>

          <el-tooltip :content="themeStore.isDark ? '切换到浅色' : '切换到深色'" placement="right">
            <button class="icon-btn sidebar-icon-btn" type="button" @click="toggleTheme">
              <el-icon :size="18">
                <Sunny v-if="themeStore.isDark" />
                <Moon v-else />
              </el-icon>
            </button>
          </el-tooltip>
        </div>

        <div class="user-info" @click="handleUserInfoClick">
          <el-avatar :size="34" :src="userStore.userInfo?.avatar">
            {{ userStore.userInfo?.username?.charAt(0)?.toUpperCase() }}
          </el-avatar>
          <div v-show="!isCollapsed" class="user-copy">
            <span class="username">{{ userStore.userInfo?.username }}</span>
            <span class="user-link">个人中心</span>
          </div>
          <el-icon v-show="!isCollapsed" class="dropdown-icon">
            <ArrowDown />
          </el-icon>
        </div>

        <transition name="slide-up">
          <div v-if="showUserMenu && !isCollapsed" class="user-menu">
            <router-link to="/profile" class="menu-item">
              <el-icon><User /></el-icon>
              <span>个人中心</span>
            </router-link>
            <div class="menu-item" @click="handleLogout">
              <el-icon><SwitchButton /></el-icon>
              <span>退出登录</span>
            </div>
          </div>
        </transition>
      </div>
    </aside>

    <main class="stage">
      <header class="topbar">
        <div class="topbar-left">
          <button class="collapse-btn" type="button" @click="isCollapsed = !isCollapsed">
            <el-icon :size="18">
              <Fold v-if="!isCollapsed" />
              <Expand v-else />
            </el-icon>
          </button>

          <div class="title-block">
            <h1>{{ currentPageTitle }}</h1>
          </div>
        </div>

        <div class="topbar-right">
          <el-select
            v-if="userStore.workspaces?.length"
            :model-value="userStore.currentWorkspaceId || undefined"
            class="workspace-select"
            placeholder="选择工作空间"
            @change="handleWorkspaceChange"
          >
            <el-option
              v-for="workspace in userStore.workspaces"
              :key="workspace.id"
              :label="workspace.name"
              :value="workspace.id"
            />
          </el-select>
        </div>
      </header>

      <section class="content-wrapper" :class="{ 'content-wrapper--pipeline-detail': isPipelineDetailPage }">
        <router-view />
      </section>
    </main>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessageBox } from 'element-plus'
import { useUserStore } from '@/stores/user'
import { useNotificationStore } from '@/stores/notification'
import { useThemeStore } from '@/stores/theme'
import {
  House,
  Connection,
  Box,
  Promotion,
  DataAnalysis,
  Setting,
  Bell,
  User,
  SwitchButton,
  ArrowDown,
  Fold,
  Expand,
  Monitor,
  Key,
  Collection,
  Shop,
  Sunny,
  Moon
} from '@element-plus/icons-vue'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()
const notificationStore = useNotificationStore()
const themeStore = useThemeStore()

const isCollapsed = ref(false)
const showUserMenu = ref(false)

onMounted(() => {
  themeStore.init()
  notificationStore.startPolling()
})

onUnmounted(() => {
  notificationStore.stopPolling()
})

const toggleTheme = () => {
  themeStore.toggleTheme()
}

const menuItems = [
  { name: '工作台', path: '/', icon: House },
  { name: '流水线', path: '/pipeline', icon: Connection, permission: 'pipeline.read' },
  { name: '项目', path: '/project', icon: Box, permission: 'project.read' },
  { name: '商店', path: '/store', icon: Shop, permission: 'store.template.read' },
  { name: '执行器', path: '/agent', icon: Monitor, permission: 'agent.read' },
  { name: '资源管理', path: '/resources', icon: Collection, permission: 'resource.read' },
  { name: '发布', path: '/deploy', icon: Promotion },
  { name: '凭据管理', path: '/credentials', icon: Key, permission: 'credential.read' },
  { name: '统计', path: '/statistics', icon: DataAnalysis, permission: 'workspace.read' },
  { name: '设置', path: '/settings', icon: Setting, permission: 'workspace.read' }
]

const pageTitleMatchers = [
  { name: '工作台', match: (path) => path === '/' },
  { name: '流水线', match: (path) => path === '/pipeline' || path.startsWith('/pipeline/') },
  { name: '项目', match: (path) => path === '/project' || path.startsWith('/project/') },
  { name: '商店', match: (path) => path === '/store' || path.startsWith('/store/') },
  { name: '执行器', match: (path) => path === '/agent' || path.startsWith('/agent/') },
  { name: '资源管理', match: (path) => path === '/resources' || path.startsWith('/resources/') || path === '/terminal' },
  { name: '发布', match: (path) => path === '/deploy' || path.startsWith('/deploy/') },
  { name: '凭据管理', match: (path) => path === '/credentials' || path.startsWith('/credentials/') },
  { name: '统计', match: (path) => path === '/statistics' || path.startsWith('/statistics/') },
  { name: '设置', match: (path) => path === '/settings' || path.startsWith('/settings/') },
  { name: '消息', match: (path) => path === '/messages' || path.startsWith('/messages/') },
  { name: '个人中心', match: (path) => path === '/profile' || path.startsWith('/profile/') }
]

const filteredMenuItems = computed(() => menuItems.filter((item) => !item.permission || userStore.hasPermission(item.permission)))

const currentPageTitle = computed(() => {
  const matchedItem = pageTitleMatchers.find((item) => item.match(route.path))
  return matchedItem?.name || '工作台'
})

const isPipelineDetailPage = computed(() => route.path.startsWith('/pipeline/') && route.path !== '/pipeline')

watch(() => userStore.currentWorkspaceId, async () => {
  await notificationStore.refreshUnreadCount()
}, { immediate: true })

watch(isCollapsed, (collapsed) => {
  if (collapsed) {
    showUserMenu.value = false
  }
})

watch(() => route.fullPath, () => {
  showUserMenu.value = false
})

const isActive = (path) => {
  if (path === '/') {
    return route.path === '/'
  }
  return route.path === path || route.path.startsWith(path + '/')
}

const handleUserInfoClick = () => {
  if (isCollapsed.value) {
    router.push('/profile')
    return
  }
  showUserMenu.value = !showUserMenu.value
}

const handleWorkspaceChange = async (workspaceId) => {
  userStore.setCurrentWorkspaceById(workspaceId)
  await userStore.getUserInfoAction()
  if (route.meta.permission && !userStore.hasPermission(route.meta.permission)) {
    router.push('/')
  }
}

const handleLogout = async () => {
  try {
    await ElMessageBox.confirm('确定要退出登录吗？', '提示', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    })

    await userStore.doLogout()
    router.push('/login')
  } catch {
    // 用户取消
  }
}
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.layout-shell {
  position: relative;
  display: flex;
  width: 100%;
  height: 100vh;
  overflow: hidden;
  background: transparent;
}

.layout-ambience {
  position: absolute;
  inset: 0;
  z-index: 0;
  pointer-events: none;

  .orb {
    position: absolute;
    border-radius: 50%;
    filter: blur(54px);
    opacity: 0.5;
    animation: drift 20s ease-in-out infinite;
  }

  .orb-a {
    width: 380px;
    height: 380px;
    left: -120px;
    top: -110px;
    background: rgba($primary-color, 0.32);
  }

  .orb-b {
    width: 300px;
    height: 300px;
    right: 12%;
    top: -80px;
    background: rgba($info-color, 0.26);
    animation-delay: -7s;
  }

  .orb-c {
    width: 320px;
    height: 320px;
    right: -90px;
    bottom: -110px;
    background: rgba($primary-color, 0.22);
    animation-delay: -12s;
  }
}

@keyframes drift {
  0%,
  100% {
    transform: translate3d(0, 0, 0);
  }
  50% {
    transform: translate3d(-18px, 14px, 0);
  }
}

.sidebar {
  position: relative;
  z-index: 2;
  width: $sidebar-width;
  margin: 8px 0 8px 8px;
  padding: 8px;
  display: flex;
  flex-direction: column;
  border-radius: $radius-2xl;
  background: var(--bg-sidebar);
  border: 1px solid var(--glass-border);
  box-shadow: var(--shadow-lg);
  backdrop-filter: $blur-lg;
  -webkit-backdrop-filter: $blur-lg;
  transition: width $transition-slow;

  &.collapsed {
    width: $sidebar-collapsed-width;

    .sidebar-header {
      padding: 0 6px;
    }

    .sidebar-brand {
      justify-content: center;
    }

    .nav-item {
      justify-content: center;
      padding: 14px 0;
    }

    .sidebar-footer {
      align-items: center;
    }

    .sidebar-footer-icons {
      justify-content: center;
    }

    .user-info {
      justify-content: center;
      padding: 10px;
    }
  }
}

.sidebar-header {
  height: $header-height;
  display: flex;
  align-items: center;
  padding: 0 14px;
  margin-bottom: 4px;
}

.sidebar-brand {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;

  .logo {
    width: 38px;
    height: 38px;
    flex-shrink: 0;
    filter: drop-shadow(0 6px 12px rgba($primary-color, 0.22));
  }

  .brand-text {
    display: flex;
    min-width: 0;
    overflow: hidden;

    .title {
      font-family: $font-family-display;
      font-size: 20px;
      font-weight: 750;
      letter-spacing: -0.03em;
      color: var(--text-primary);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
  }
}

.sidebar-nav {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow-y: auto;
  padding: 8px 6px;
}

.nav-section {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.nav-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 0 4px;
  padding: 12px 14px;
  border-radius: $radius-lg;
  color: var(--text-secondary);
  text-decoration: none;
  font-weight: 600;
  transition: color $transition-fast, background $transition-fast, transform $transition-fast;

  &::before {
    content: '';
    position: absolute;
    inset: 0;
    border-radius: inherit;
    border: 1px solid transparent;
    transition: border-color $transition-fast;
  }

  &:hover {
    color: var(--primary-color);
    background: linear-gradient(130deg, rgba($primary-color, 0.12) 0%, rgba($primary-color, 0.03) 100%);
    transform: translateX(2px);

    &::before {
      border-color: rgba($primary-color, 0.24);
    }
  }

  &.active {
    color: var(--primary-color);
    background: linear-gradient(130deg, rgba($primary-color, 0.2) 0%, rgba($primary-color, 0.07) 100%);
    box-shadow: 0 8px 18px rgba($primary-color, 0.18);

    &::before {
      border-color: rgba($primary-color, 0.34);
    }
  }

  .nav-icon {
    font-size: 18px;
    flex-shrink: 0;
  }

  .nav-text {
    font-size: 14px;
    white-space: nowrap;
  }
}

.sidebar-footer {
  padding: 12px 6px 4px;
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 10px;
  border-top: 1px solid var(--border-color-light);
}

.sidebar-footer-icons {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 4px;
}

.sidebar-icon-btn {
  width: 38px;
  height: 38px;
}

.sidebar-badge {
  :deep(.el-badge__content) {
    border: none;
    box-shadow: 0 0 0 2px var(--bg-elevated);
    background: $danger-color;
  }
}

.user-info {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  min-width: 0;
  border-radius: $radius-lg;
  background: var(--bg-elevated);
  border: 1px solid var(--border-color-light);
  box-shadow: var(--shadow-sm);
  padding: 10px 12px;
  cursor: pointer;
  transition: transform $transition-fast, box-shadow $transition-fast;

  &:hover {
    transform: translateY(-1px);
    box-shadow: var(--shadow-md);
  }

  :deep(.el-avatar) {
    flex-shrink: 0;
    background: linear-gradient(135deg, $primary-color 0%, $primary-hover 100%);
    font-weight: 650;
  }

  .user-copy {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .username {
    min-width: 0;
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .user-link {
    font-size: 12px;
    color: var(--text-tertiary);
    white-space: nowrap;
  }

  .dropdown-icon {
    color: var(--text-muted);
    font-size: 12px;
    flex-shrink: 0;
  }
}

.user-menu {
  position: absolute;
  left: 6px;
  right: 6px;
  bottom: calc(100% + 8px);
  border-radius: $radius-lg;
  border: 1px solid var(--border-color-light);
  background: var(--bg-elevated);
  box-shadow: var(--shadow-md);
  backdrop-filter: $blur-md;
  -webkit-backdrop-filter: $blur-md;
  padding: 8px;

  .menu-item {
    display: flex;
    align-items: center;
    gap: 10px;
    border-radius: $radius-md;
    padding: 10px 12px;
    color: var(--text-secondary);
    cursor: pointer;
    transition: all $transition-fast;
    font-size: 13px;
    font-weight: 600;

    &:hover {
      color: var(--primary-color);
      background: var(--primary-lighter);
    }
  }
}

.slide-up-enter-active,
.slide-up-leave-active {
  transition: all $transition-base;
}

.slide-up-enter-from,
.slide-up-leave-to {
  opacity: 0;
  transform: translateY(10px);
}

.stage {
  position: relative;
  z-index: 2;
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  margin: 8px;
  border-radius: $radius-2xl;
  overflow: hidden;
}

.topbar {
  height: $header-height;
  padding: 0 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  border: 1px solid var(--glass-border);
  border-radius: $radius-2xl;
  background: var(--glass-bg);
  box-shadow: var(--shadow-md);
  backdrop-filter: $blur-md;
  -webkit-backdrop-filter: $blur-md;
}

.topbar-left {
  display: flex;
  align-items: center;
  gap: 14px;
  min-width: 0;
  flex: 1;
}

.collapse-btn {
  width: 36px;
  height: 36px;
  border: 1px solid var(--border-color-light);
  border-radius: $radius-md;
  background: var(--bg-elevated);
  color: var(--text-secondary);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  box-shadow: var(--shadow-sm);
  transition: all $transition-fast;

  &:hover {
    color: var(--primary-color);
    border-color: var(--border-color-hover);
    transform: translateY(-1px);
  }
}

.title-block {
  min-width: 0;
  flex: 1;

  h1 {
    margin: 0;
    font-family: $font-family-display;
    font-size: 22px;
    line-height: 1.12;
    font-weight: 720;
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
}

.topbar-right {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  flex-shrink: 1;
}

.workspace-select {
  width: min(280px, 32vw);
  min-width: 140px;
  max-width: 100%;
  flex-shrink: 1;

  :deep(.el-input__wrapper) {
    min-width: 0;
  }

  :deep(.el-select__selected-item),
  :deep(.el-input__inner) {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}

.icon-btn {
  width: 34px;
  height: 34px;
  border: 1px solid var(--border-color-light);
  border-radius: $radius-md;
  background: var(--bg-elevated);
  color: var(--text-secondary);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  box-shadow: var(--shadow-sm);
  transition: all $transition-fast;

  &:hover {
    color: var(--primary-color);
    border-color: var(--border-color-hover);
    transform: translateY(-1px);
  }
}

.content-wrapper {
  flex: 1;
  margin-top: 8px;
  padding: 0 10px 10px;
  border-radius: $radius-2xl;
  border: 1px solid var(--glass-border);
  background: var(--glass-bg);
  backdrop-filter: $blur-sm;
  -webkit-backdrop-filter: $blur-sm;
  overflow: auto;

  &.content-wrapper--pipeline-detail {
    margin-top: 8px;
    padding: 0;
    border: none;
    border-radius: 0;
    background: transparent;
    backdrop-filter: none;
    -webkit-backdrop-filter: none;
    overflow-x: hidden;
    overflow-y: auto;
  }
}

@media (max-width: 1200px) {
  .workspace-select {
    width: min(220px, 28vw);
  }
}

@media (max-width: 992px) {
  .sidebar {
    position: absolute;
    height: calc(100% - 28px);
  }

  .stage {
    margin-left: 96px;
  }

  .title-block h1 {
    font-size: 18px;
  }

  .topbar {
    height: 64px;
    padding: 0 14px;
  }

  .workspace-select {
    width: min(180px, 24vw);
    min-width: 120px;
  }

  .content-wrapper {
    padding: 14px;
  }
}

@media (max-width: 720px) {
  .topbar {
    gap: 10px;
  }

  .workspace-select {
    width: min(148px, 22vw);
    min-width: 104px;
  }
}

@media (max-width: 560px) {
  .sidebar-footer-icons {
    gap: 6px;
  }
}
</style>

<template>
  <div class="layout-shell">
    <div class="layout-ambience" aria-hidden="true">
      <span class="orb orb-a"></span>
      <span class="orb orb-b"></span>
      <span class="orb orb-c"></span>
    </div>

    <aside class="sidebar" :class="{ collapsed: isCollapsed }">
      <div class="sidebar-header">
        <img src="@/assets/images/logo.svg" alt="Logo" class="logo" />
        <div v-show="!isCollapsed" class="brand-text">
          <span class="title">EasyDo</span>
          <span class="subtitle">Delivery Control</span>
        </div>
      </div>

      <nav class="sidebar-nav">
        <div class="nav-section">
          <router-link
            v-for="item in menuItems"
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

        <div class="nav-section bottom">
          <router-link
            v-for="item in bottomMenuItems"
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
        <div class="user-info" @click="showUserMenu = !showUserMenu">
          <el-avatar :size="34" :src="userStore.userInfo?.avatar">
            {{ userStore.userInfo?.username?.charAt(0)?.toUpperCase() }}
          </el-avatar>
          <span v-show="!isCollapsed" class="username">{{ userStore.userInfo?.username }}</span>
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
            <el-breadcrumb separator="/">
              <el-breadcrumb-item :to="{ path: '/' }">首页</el-breadcrumb-item>
              <el-breadcrumb-item>{{ currentPageTitle }}</el-breadcrumb-item>
            </el-breadcrumb>
          </div>
        </div>

        <div class="topbar-right">
          <div class="time-chip">
            <el-icon :size="15"><Calendar /></el-icon>
            <span>{{ currentDateLabel }}</span>
          </div>

          <el-tooltip :content="themeStore.isDark ? '切换到浅色' : '切换到深色'" placement="bottom">
            <button class="icon-btn" type="button" @click="toggleTheme">
              <el-icon :size="18">
                <Sunny v-if="themeStore.isDark" />
                <Moon v-else />
              </el-icon>
            </button>
          </el-tooltip>

          <el-badge :value="3" :max="99" class="header-badge">
            <button class="icon-btn" type="button">
              <el-icon :size="18"><Bell /></el-icon>
            </button>
          </el-badge>

          <el-tooltip content="帮助与支持" placement="bottom">
            <button class="icon-btn" type="button">
              <el-icon :size="18"><QuestionFilled /></el-icon>
            </button>
          </el-tooltip>

          <el-dropdown trigger="click">
            <button class="quick-app-btn" type="button">
              <el-icon :size="15"><Link /></el-icon>
              <span>应用</span>
              <el-icon :size="12"><ArrowDown /></el-icon>
            </button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item>应用1</el-dropdown-item>
                <el-dropdown-item>应用2</el-dropdown-item>
                <el-dropdown-item>应用3</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>

          <el-dropdown trigger="click" @command="handleCommand">
            <button class="user-chip" type="button">
              <el-avatar :size="30" :src="userStore.userInfo?.avatar">
                {{ userStore.userInfo?.username?.charAt(0)?.toUpperCase() }}
              </el-avatar>
              <span class="user-name">{{ userStore.userInfo?.username }}</span>
              <el-icon :size="12"><ArrowDown /></el-icon>
            </button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="profile">
                  <el-icon><User /></el-icon>
                  个人中心
                </el-dropdown-item>
                <el-dropdown-item command="settings">
                  <el-icon><Setting /></el-icon>
                  系统设置
                </el-dropdown-item>
                <el-dropdown-item divided command="logout">
                  <el-icon><SwitchButton /></el-icon>
                  退出登录
                </el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </header>

      <section class="content-wrapper">
        <router-view />
      </section>
    </main>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessageBox } from 'element-plus'
import { useUserStore } from '@/stores/user'
import { useThemeStore } from '@/stores/theme'
import {
  House,
  Connection,
  Box,
  Promotion,
  DataAnalysis,
  Setting,
  Bell,
  QuestionFilled,
  User,
  SwitchButton,
  ArrowDown,
  Fold,
  Expand,
  Link,
  Monitor,
  Key,
  Sunny,
  Moon,
  Calendar
} from '@element-plus/icons-vue'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()
const themeStore = useThemeStore()

const isCollapsed = ref(false)
const showUserMenu = ref(false)
const currentDate = ref(new Date())
let dateTimer = null

onMounted(() => {
  themeStore.init()
  dateTimer = setInterval(() => {
    currentDate.value = new Date()
  }, 60000)
})

onUnmounted(() => {
  if (dateTimer) {
    clearInterval(dateTimer)
  }
})

const currentDateLabel = computed(() => {
  return currentDate.value.toLocaleDateString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    weekday: 'short'
  })
})

const toggleTheme = () => {
  themeStore.toggleTheme()
}

const menuItems = [
  { name: '工作台', path: '/', icon: House },
  { name: '流水线', path: '/pipeline', icon: Connection },
  { name: '项目', path: '/project', icon: Box },
  { name: '执行器', path: '/agent', icon: Monitor },
  { name: '发布', path: '/deploy', icon: Promotion },
  { name: '密钥管理', path: '/secrets', icon: Key },
  { name: '统计', path: '/statistics', icon: DataAnalysis },
  { name: '设置', path: '/settings', icon: Setting }
]

const bottomMenuItems = [
  { name: '消息', path: '/messages', icon: Bell }
]

const currentPageTitle = computed(() => {
  const currentRoute = [...menuItems, ...bottomMenuItems].find(item => {
    if (item.path === '/') {
      return route.path === '/'
    }
    return route.path === item.path || route.path.startsWith(item.path + '/')
  })
  return currentRoute?.name || '工作台'
})

const isActive = (path) => {
  if (path === '/') {
    return route.path === '/'
  }
  return route.path === path || route.path.startsWith(path + '/')
}

const handleCommand = (command) => {
  switch (command) {
    case 'profile':
      router.push('/profile')
      break
    case 'settings':
      router.push('/settings')
      break
    case 'logout':
      handleLogout()
      break
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
  margin: 14px 0 14px 14px;
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
      justify-content: center;
      padding: 0;
    }

    .nav-item {
      justify-content: center;
      padding: 14px 0;
    }
  }
}

.sidebar-header {
  height: $header-height;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 0 14px;
  margin-bottom: 4px;

  .logo {
    width: 38px;
    height: 38px;
    flex-shrink: 0;
    filter: drop-shadow(0 6px 12px rgba($primary-color, 0.22));
  }

  .brand-text {
    display: flex;
    flex-direction: column;
    overflow: hidden;

    .title {
      font-family: $font-family-display;
      font-size: 20px;
      font-weight: 750;
      letter-spacing: -0.03em;
      color: var(--text-primary);
    }

    .subtitle {
      font-size: 11px;
      color: var(--text-tertiary);
      letter-spacing: 0.08em;
      text-transform: uppercase;
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

  &.bottom {
    margin-top: auto;
    padding-top: 12px;
    border-top: 1px solid var(--border-color-light);
  }
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
  padding: 8px 6px 4px;
  position: relative;
}

.user-info {
  display: flex;
  align-items: center;
  gap: 10px;
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
    background: linear-gradient(135deg, $primary-color 0%, $primary-hover 100%);
    font-weight: 650;
  }

  .username {
    flex: 1;
    min-width: 0;
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .dropdown-icon {
    color: var(--text-muted);
    font-size: 12px;
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
  margin: 14px;
  border-radius: $radius-2xl;
  overflow: hidden;
}

.topbar {
  height: $header-height;
  padding: 0 20px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
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

  h1 {
    font-family: $font-family-display;
    font-size: 22px;
    line-height: 1.12;
    font-weight: 720;
    letter-spacing: -0.02em;
    margin-bottom: 6px;
    color: var(--text-primary);
  }

  :deep(.el-breadcrumb__item .el-breadcrumb__inner) {
    font-size: 12px;
  }
}

.topbar-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.time-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 34px;
  padding: 0 12px;
  border-radius: $radius-full;
  border: 1px solid var(--border-color-light);
  background: var(--bg-elevated);
  color: var(--text-secondary);
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.01em;
  box-shadow: var(--shadow-sm);
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

.header-badge {
  :deep(.el-badge__content) {
    border: none;
    box-shadow: 0 0 0 2px var(--bg-elevated);
    background: $danger-color;
  }
}

.quick-app-btn {
  height: 34px;
  border: 1px solid var(--border-color-light);
  border-radius: $radius-md;
  background: var(--bg-elevated);
  color: var(--text-secondary);
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 0 12px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  box-shadow: var(--shadow-sm);
  transition: all $transition-fast;

  &:hover {
    color: var(--primary-color);
    transform: translateY(-1px);
  }
}

.user-chip {
  height: 36px;
  border: 1px solid var(--border-color-light);
  border-radius: $radius-full;
  background: var(--bg-elevated);
  color: var(--text-secondary);
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 0 8px 0 4px;
  cursor: pointer;
  box-shadow: var(--shadow-sm);
  transition: all $transition-fast;

  &:hover {
    transform: translateY(-1px);
    border-color: var(--border-color-hover);
    box-shadow: var(--shadow-md);
  }

  .user-name {
    max-width: 110px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
  }
}

.content-wrapper {
  flex: 1;
  margin-top: 12px;
  padding: 20px;
  border-radius: $radius-2xl;
  border: 1px solid var(--glass-border);
  background: rgba(255, 255, 255, 0.34);
  backdrop-filter: $blur-sm;
  -webkit-backdrop-filter: $blur-sm;
  overflow: auto;
}

@media (max-width: 1200px) {
  .time-chip,
  .quick-app-btn span,
  .user-chip .user-name {
    display: none;
  }

  .quick-app-btn,
  .user-chip {
    padding: 0 8px;
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
    margin-bottom: 3px;
  }

  .topbar {
    height: 64px;
    padding: 0 14px;
  }

  .content-wrapper {
    padding: 14px;
  }
}
</style>

<template>
  <div class="layout-container">
    <!-- 侧边栏 -->
    <aside class="sidebar" :class="{ collapsed: isCollapsed }">
      <div class="sidebar-header">
        <img src="@/assets/images/logo.svg" alt="Logo" class="logo" />
        <span v-show="!isCollapsed" class="title">EasyDo</span>
      </div>
      
      <nav class="sidebar-nav">
        <div class="nav-section">
          <template v-for="item in menuItems" :key="item.path">
            <router-link
              :to="item.path"
              class="nav-item"
              :class="{ active: isActive(item.path) }"
            >
              <el-icon class="nav-icon">
                <component :is="item.icon" />
              </el-icon>
              <span v-show="!isCollapsed" class="nav-text">{{ item.name }}</span>
            </router-link>
          </template>
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
          <el-avatar :size="32" :src="userStore.userInfo?.avatar">
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
    
    <!-- 主内容区 -->
    <main class="main-content">
      <header class="header">
        <div class="header-left">
          <el-icon class="collapse-btn" @click="isCollapsed = !isCollapsed">
            <Fold v-if="!isCollapsed" />
            <Expand v-else />
          </el-icon>
          <el-breadcrumb separator="/">
            <el-breadcrumb-item :to="{ path: '/' }">首页</el-breadcrumb-item>
            <el-breadcrumb-item>{{ currentPageTitle }}</el-breadcrumb-item>
          </el-breadcrumb>
        </div>
        
        <div class="header-right">
          <el-tooltip :content="themeStore.isDark ? '切换到浅色' : '切换到深色'" placement="bottom">
            <div class="header-icon theme-toggle" @click="toggleTheme">
              <el-icon :size="20">
                <Sunny v-if="themeStore.isDark" />
                <Moon v-else />
              </el-icon>
            </div>
          </el-tooltip>
          <el-badge :value="3" :max="99" class="header-badge">
            <el-icon :size="20"><Bell /></el-icon>
          </el-badge>
          <el-tooltip content="帮助与支持" placement="bottom">
            <el-icon :size="20" class="header-icon"><QuestionFilled /></el-icon>
          </el-tooltip>
          <el-dropdown trigger="click">
            <div class="header-dropdown">
              <el-icon :size="16"><Link /></el-icon>
              <span class="dropdown-text">应用</span>
              <el-icon><ArrowDown /></el-icon>
            </div>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item>应用1</el-dropdown-item>
                <el-dropdown-item>应用2</el-dropdown-item>
                <el-dropdown-item>应用3</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
          <el-dropdown trigger="click" @command="handleCommand">
            <div class="user-dropdown">
              <el-avatar :size="28" :src="userStore.userInfo?.avatar">
                {{ userStore.userInfo?.username?.charAt(0)?.toUpperCase() }}
              </el-avatar>
              <span class="user-name">{{ userStore.userInfo?.username }}</span>
              <el-icon><ArrowDown /></el-icon>
            </div>
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
      
      <div class="content-wrapper">
        <router-view />
      </div>
    </main>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessageBox } from 'element-plus'
import { useUserStore } from '@/stores/user'
import { useThemeStore } from '@/stores/theme'
import {
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
  Moon
} from '@element-plus/icons-vue'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()
const themeStore = useThemeStore()

const isCollapsed = ref(false)
const showUserMenu = ref(false)

onMounted(() => {
  themeStore.init()
})

const toggleTheme = () => {
  themeStore.toggleTheme()
}

const menuItems = [
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
  const currentRoute = [...menuItems, ...bottomMenuItems].find(
    item => route.path.startsWith(item.path)
  )
  return currentRoute?.name || '首页'
})

const isActive = (path) => {
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

.layout-container {
  display: flex;
  width: 100%;
  height: 100vh;
  background: var(--bg-primary);
}

// ============================================
// Modern Sidebar with Neumorphic Style
// ============================================
.sidebar {
  width: $sidebar-width;
  height: 100%;
  background: var(--bg-sidebar);
  display: flex;
  flex-direction: column;
  transition: width $transition-slow;
  position: relative;
  z-index: 100;
  box-shadow: 
    4px 0 24px rgba(0, 0, 0, 0.04),
    inset -1px 0 0 rgba(255, 255, 255, 0.6);
  
  &.collapsed {
    width: $sidebar-collapsed-width;
    
    .nav-item {
      justify-content: center;
      padding: 14px 0;
      margin: 6px 12px;
    }
    
    .sidebar-header {
      padding: 0;
      justify-content: center;
    }
  }
}

.sidebar-header {
  height: $header-height;
  display: flex;
  align-items: center;
  padding: 0 24px;
  overflow: hidden;
  
  .logo {
    width: 36px;
    height: 36px;
    flex-shrink: 0;
    filter: drop-shadow(0 2px 6px rgba($primary-color, 0.2));
  }
  
  .title {
    font-family: $font-family-display;
    font-size: 20px;
    font-weight: 700;
    color: var(--text-primary);
    margin-left: 12px;
    white-space: nowrap;
    letter-spacing: -0.02em;
  }
}

.sidebar-nav {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow-y: auto;
  padding: 16px 12px;
  gap: 4px;
}

.nav-section {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 4px;
  
  &.bottom {
    padding-top: 12px;
    margin-top: 12px;
    border-top: 1px solid var(--border-color);
  }
}

.nav-item {
  display: flex;
  align-items: center;
  padding: 14px 18px;
  color: var(--text-secondary);
  text-decoration: none;
  transition: all $transition-base;
  cursor: pointer;
  margin: 2px 8px;
  border-radius: $radius-md;
  font-weight: 500;
  
  &:hover {
    color: $primary-color;
    background: rgba($primary-color, 0.06);
  }
  
  &.active {
    color: $primary-color;
    background: linear-gradient(135deg, rgba($primary-color, 0.12) 0%, rgba($primary-color, 0.06) 100%);
    box-shadow: 
      inset 0 0 0 1px rgba($primary-color, 0.15),
      $shadow-sm;
  }
  
  .nav-icon {
    font-size: 20px;
    flex-shrink: 0;
    transition: transform $transition-fast;
  }
  
  &:hover .nav-icon {
    transform: scale(1.1);
  }
  
  .nav-text {
    margin-left: 14px;
    font-size: 14px;
    white-space: nowrap;
  }
}

.sidebar-footer {
  padding: 16px;
  border-top: 1px solid var(--border-color);
  position: relative;
}

.user-info {
  display: flex;
  align-items: center;
  padding: 10px 14px;
  border-radius: $radius-md;
  cursor: pointer;
  color: var(--text-secondary);
  transition: all $transition-base;
  background: var(--bg-secondary);
  box-shadow: var(--shadow-sm);
  
  &:hover {
    background: var(--bg-card);
    box-shadow: var(--shadow-md);
    color: var(--text-primary);
  }
  
  :deep(.el-avatar) {
    background: linear-gradient(135deg, $primary-color 0%, $primary-hover 100%);
    font-weight: 600;
  }
  
  .username {
    margin-left: 12px;
    font-size: 14px;
    font-weight: 500;
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  
  .dropdown-icon {
    font-size: 12px;
    color: var(--text-muted);
  }
}

.user-menu {
  position: absolute;
  bottom: calc(100% + 8px);
  left: 16px;
  right: 16px;
  background: var(--glass-bg);
  backdrop-filter: $blur-md;
  -webkit-backdrop-filter: $blur-md;
  border-radius: $radius-lg;
  box-shadow: var(--shadow-lg);
  padding: 8px;
  border: 1px solid $glass-border;
  
  .menu-item {
    display: flex;
    align-items: center;
    padding: 12px 16px;
    color: var(--text-secondary);
    cursor: pointer;
    transition: all $transition-fast;
    border-radius: $radius-md;
    font-size: 14px;
    font-weight: 500;
    
    &:hover {
      background: rgba($primary-color, 0.08);
      color: $primary-color;
    }
    
    .el-icon {
      margin-right: 10px;
      font-size: 16px;
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

// ============================================
// Main Content Area
// ============================================
.main-content {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--bg-primary);
}

// ============================================
// Glassmorphism Header
// ============================================
.header {
  height: $header-height;
  background: var(--glass-bg);
  backdrop-filter: $blur-md;
  -webkit-backdrop-filter: $blur-md;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 24px;
  box-shadow: 
    0 1px 3px rgba(0, 0, 0, 0.04),
    inset 0 -1px 0 rgba(255, 255, 255, 0.6);
  z-index: 99;
  border-bottom: 1px solid $glass-border;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 20px;
  
  .collapse-btn {
    width: 36px;
    height: 36px;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 18px;
    cursor: pointer;
    color: var(--text-secondary);
    border-radius: $radius-md;
    transition: all $transition-fast;
    
    &:hover {
      color: $primary-color;
      background: rgba($primary-color, 0.08);
    }
  }
  
  :deep(.el-breadcrumb) {
    .el-breadcrumb__item {
      .el-breadcrumb__inner {
        color: var(--text-tertiary);
        font-weight: 500;
        
        &.is-link {
          color: var(--text-secondary);
          
          &:hover {
            color: $primary-color;
          }
        }
      }
      
      &:last-child .el-breadcrumb__inner {
        color: var(--text-primary);
        font-weight: 600;
      }
    }
  }
}

.header-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.header-badge {
  cursor: pointer;
  padding: 8px;
  border-radius: $radius-md;
  transition: all $transition-fast;
  color: var(--text-secondary);
  
  &:hover {
    background: rgba($primary-color, 0.08);
    color: $primary-color;
  }
  
  :deep(.el-badge__content) {
    background: $danger-color;
    border: none;
    box-shadow: 0 2px 4px rgba($danger-color, 0.3);
  }
}

.header-icon {
  cursor: pointer;
  color: var(--text-secondary);
  padding: 8px;
  border-radius: $radius-md;
  transition: all $transition-fast;
  
  &:hover {
    background: rgba($primary-color, 0.08);
    color: $primary-color;
  }
}

.header-dropdown {
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  padding: 8px 12px;
  border-radius: $radius-md;
  transition: all $transition-fast;
  color: var(--text-secondary);
  
  &:hover {
    background: rgba($primary-color, 0.08);
    color: $primary-color;
  }
  
  .dropdown-text {
    font-size: 14px;
    font-weight: 500;
  }
}

.user-dropdown {
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  padding: 6px 12px 6px 6px;
  border-radius: $radius-full;
  transition: all $transition-fast;
  background: var(--bg-secondary);
  box-shadow: var(--shadow-sm);
  
  &:hover {
    background: var(--bg-card);
    box-shadow: var(--shadow-md);
  }
  
  :deep(.el-avatar) {
    background: linear-gradient(135deg, $primary-color 0%, $primary-hover 100%);
    font-weight: 600;
  }
  
  .user-name {
    font-size: 14px;
    color: var(--text-primary);
    font-weight: 500;
    max-width: 100px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}

.content-wrapper {
  flex: 1;
  overflow: auto;
  padding: 24px;
  background: var(--bg-primary);
}
</style>

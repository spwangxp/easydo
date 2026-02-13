<template>
  <div class="settings-container">
    <div class="settings-header">
      <h1 class="page-title">设置</h1>
    </div>
    
    <div class="settings-layout">
      <aside class="settings-sidebar">
        <div 
          v-for="item in menuItems" 
          :key="item.key"
          class="menu-item"
          :class="{ active: activeMenu === item.key }"
          @click="activeMenu = item.key"
        >
          <el-icon><component :is="item.icon" /></el-icon>
          <span>{{ item.name }}</span>
        </div>
      </aside>
      
      <main class="settings-content">
        <!-- 基本设置 -->
        <div v-if="activeMenu === 'basic'" class="settings-section">
          <h2 class="section-title">基本设置</h2>
          
          <div class="form-group">
            <label>系统名称</label>
            <el-input v-model="settings.systemName" placeholder="请输入系统名称" />
          </div>
          
          <div class="form-group">
            <label>系统 Logo</label>
            <div class="logo-upload">
              <el-icon :size="40"><Upload /></el-icon>
              <span>点击上传 Logo</span>
            </div>
          </div>
          
          <div class="form-group">
            <label>系统主题</label>
            <el-radio-group v-model="settings.theme">
              <el-radio-button label="light">浅色主题</el-radio-button>
              <el-radio-button label="dark">深色主题</el-radio-button>
            </el-radio-group>
          </div>
          
          <div class="form-actions">
            <el-button type="primary" @click="saveSettings">保存设置</el-button>
          </div>
        </div>
        
        <!-- 安全设置 -->
        <div v-if="activeMenu === 'security'" class="settings-section">
          <h2 class="section-title">安全设置</h2>
          
          <div class="security-item">
            <div class="security-info">
              <h4>登录密码</h4>
              <p>定期修改密码可以提高账户安全性</p>
            </div>
            <el-button @click="showPasswordDialog = true">修改</el-button>
          </div>
          
          <div class="security-item">
            <div class="security-info">
              <h4>两步验证</h4>
              <p>开启两步验证后，登录时需要输入验证码</p>
            </div>
            <el-switch v-model="settings.twoFactorEnabled" />
          </div>
          
          <div class="security-item">
            <div class="security-info">
              <h4>登录设备管理</h4>
              <p>查看和管理已登录的设备</p>
            </div>
            <el-button @click="showDevicesDialog = true">查看</el-button>
          </div>
        </div>
        
        <!-- 通知设置 -->
        <div v-if="activeMenu === 'notifications'" class="settings-section">
          <h2 class="section-title">通知设置</h2>
          
          <div class="notification-group">
            <h4>邮件通知</h4>
            
            <div class="notification-item">
              <div class="notification-info">
                <span class="notification-label">构建失败通知</span>
                <span class="notification-desc">流水线构建失败时发送邮件</span>
              </div>
              <el-switch v-model="notifications.buildFailed" />
            </div>
            
            <div class="notification-item">
              <div class="notification-info">
                <span class="notification-label">构建成功通知</span>
                <span class="notification-desc">流水线构建成功时发送邮件</span>
              </div>
              <el-switch v-model="notifications.buildSuccess" />
            </div>
            
            <div class="notification-item">
              <div class="notification-info">
                <span class="notification-label">发布通知</span>
                <span class="notification-desc">发布任务完成时发送邮件</span>
              </div>
              <el-switch v-model="notifications.deployComplete" />
            </div>
          </div>
          
          <div class="form-actions">
            <el-button type="primary" @click="saveNotifications">保存设置</el-button>
          </div>
        </div>
        
        <!-- 用户管理 -->
        <div v-if="activeMenu === 'users'" class="settings-section">
          <h2 class="section-title">用户管理</h2>
          
          <div class="users-header">
            <el-input
              placeholder="搜索用户"
              prefix-icon="Search"
              style="width: 240px"
            />
            <el-button type="primary">添加用户</el-button>
          </div>
          
          <el-table :data="users" style="width: 100%">
            <el-table-column prop="username" label="用户名" width="150" />
            <el-table-column prop="email" label="邮箱" width="200" />
            <el-table-column prop="role" label="角色" width="120">
              <template #default="{ row }">
                <el-tag :type="row.role === 'admin' ? 'danger' : 'info'" size="small">
                  {{ row.role === 'admin' ? '管理员' : '普通用户' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="status" label="状态" width="120">
              <template #default="{ row }">
                <el-tag :type="row.status === 'active' ? 'success' : 'info'" size="small">
                  {{ row.status === 'active' ? '已启用' : '已禁用' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="150">
              <template #default="{ row }">
                <el-button type="primary" link size="small">编辑</el-button>
                <el-button type="danger" link size="small">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>
        
        <!-- 第三方集成 -->
        <div v-if="activeMenu === 'integrations'" class="settings-section">
          <h2 class="section-title">第三方集成</h2>
          
          <div class="integration-list">
            <div class="integration-item">
              <div class="integration-icon dingtalk">
                <el-icon :size="24"><ChatDotRound /></el-icon>
              </div>
              <div class="integration-info">
                <h4>钉钉</h4>
                <p>集成钉钉机器人，接收构建通知</p>
              </div>
              <el-button type="primary">配置</el-button>
            </div>
            
            <div class="integration-item">
              <div class="integration-icon wechat">
                <el-icon :size="24"><ChatLineRound /></el-icon>
              </div>
              <div class="integration-info">
                <h4>企业微信</h4>
                <p>集成企业微信机器人，接收构建通知</p>
              </div>
              <el-button type="primary">配置</el-button>
            </div>
            
            <div class="integration-item">
              <div class="integration-icon ldap">
                <el-icon :size="24"><Key /></el-icon>
              </div>
              <div class="integration-info">
                <h4>LDAP</h4>
                <p>集成 LDAP 统一身份认证</p>
              </div>
              <el-button type="primary">配置</el-button>
            </div>
          </div>
        </div>
      </main>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive } from 'vue'
import { 
  Setting, 
  Lock, 
  Bell, 
  User, 
  Link,
  Upload,
  ChatDotRound,
  ChatLineRound,
  Key,
  Search
} from '@element-plus/icons-vue'

const activeMenu = ref('basic')

const menuItems = [
  { key: 'basic', name: '基本设置', icon: Setting },
  { key: 'security', name: '安全设置', icon: Lock },
  { key: 'notifications', name: '通知设置', icon: Bell },
  { key: 'users', name: '用户管理', icon: User },
  { key: 'integrations', name: '第三方集成', icon: Link }
]

const settings = reactive({
  systemName: 'EasyDo',
  theme: 'light',
  twoFactorEnabled: false
})

const notifications = reactive({
  buildFailed: true,
  buildSuccess: false,
  deployComplete: true
})

const users = ref([
  { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin', status: 'active' },
  { id: 2, username: 'demo', email: 'demo@example.com', role: 'user', status: 'active' }
])

const showPasswordDialog = ref(false)
const showDevicesDialog = ref(false)

const saveSettings = () => {
  console.log('保存设置')
}

const saveNotifications = () => {
  console.log('保存通知设置')
}
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.settings-container {
  .settings-header {
    margin-bottom: 28px;
    
    .page-title {
      font-family: $font-family-display;
      font-size: 28px;
      font-weight: 700;
      color: $text-primary;
      letter-spacing: -0.02em;
    }
  }
  
  .settings-layout {
    display: flex;
    gap: 24px;
    
    // ============================================
    // Modern Settings Sidebar
    // ============================================
    .settings-sidebar {
      width: 220px;
      background: $bg-card;
      border-radius: $radius-xl;
      padding: 16px;
      flex-shrink: 0;
      box-shadow: $shadow-md;
      
      .menu-item {
        display: flex;
        align-items: center;
        gap: 12px;
        padding: 14px 18px;
        color: $text-secondary;
        cursor: pointer;
        border-radius: $radius-md;
        transition: all $transition-base;
        font-weight: 500;
        margin-bottom: 4px;
        
        &:hover {
          background: rgba($primary-color, 0.06);
          color: $primary-color;
        }
        
        &.active {
          color: $primary-color;
          background: linear-gradient(135deg, rgba($primary-color, 0.12) 0%, rgba($primary-color, 0.06) 100%);
          box-shadow: inset 0 0 0 1px rgba($primary-color, 0.15), $shadow-sm;
        }
        
        .el-icon {
          font-size: 18px;
        }
      }
    }
    
    // ============================================
    // Modern Settings Content
    // ============================================
    .settings-content {
      flex: 1;
      background: $bg-card;
      border-radius: $radius-xl;
      padding: 32px;
      box-shadow: $shadow-md;
      
      .settings-section {
        .section-title {
          font-family: $font-family-display;
          font-size: 20px;
          font-weight: 600;
          color: $text-primary;
          margin-bottom: 28px;
          padding-bottom: 20px;
          border-bottom: 1px solid $border-color;
        }
      }
      
      .form-group {
        margin-bottom: 28px;
        
        label {
          display: block;
          font-size: 14px;
          color: $text-secondary;
          margin-bottom: 10px;
          font-weight: 500;
        }
        
        :deep(.el-input__wrapper) {
          background: $bg-secondary;
          border-radius: $radius-md;
          box-shadow: $shadow-inset;
          border: 1px solid $border-color-light;
          
          &:hover, &.is-focus {
            border-color: rgba($primary-color, 0.4);
          }
        }
        
        :deep(.el-radio-group) {
          .el-radio-button {
            &:first-child .el-radio-button__inner {
              border-radius: $radius-md 0 0 $radius-md;
            }
            &:last-child .el-radio-button__inner {
              border-radius: 0 $radius-md $radius-md 0;
            }
            
            .el-radio-button__inner {
              background: $bg-secondary;
              border-color: $border-color;
              color: $text-secondary;
              font-weight: 500;
            }
            
            &.is-active .el-radio-button__inner {
              background: linear-gradient(135deg, $primary-color 0%, $primary-hover 100%);
              border-color: $primary-color;
              color: white;
              box-shadow: none;
            }
          }
        }
      }
      
      .logo-upload {
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        width: 120px;
        height: 120px;
        border: 2px dashed $border-color;
        border-radius: $radius-lg;
        cursor: pointer;
        color: $text-muted;
        transition: all $transition-base;
        background: $bg-secondary;
        
        &:hover {
          border-color: $primary-color;
          color: $primary-color;
          background: rgba($primary-color, 0.04);
        }
      }
      
      .form-actions {
        margin-top: 32px;
        padding-top: 24px;
        border-top: 1px solid $border-color;
        
        :deep(.el-button--primary) {
          height: 44px;
          padding: 0 32px;
          border-radius: $radius-md;
          font-weight: 600;
          background: linear-gradient(135deg, $primary-color 0%, $primary-hover 100%);
          border: none;
          box-shadow: $shadow-md;
          
          &:hover {
            transform: translateY(-2px);
            box-shadow: $shadow-lg;
          }
        }
      }
      
      // ============================================
      // Security Items
      // ============================================
      .security-item {
        display: flex;
        align-items: center;
        justify-content: space-between;
        padding: 24px 0;
        border-bottom: 1px solid $border-color-light;
        
        .security-info {
          h4 {
            font-size: 15px;
            font-weight: 600;
            color: $text-primary;
            margin-bottom: 6px;
          }
          
          p {
            font-size: 13px;
            color: $text-muted;
          }
        }
        
        :deep(.el-button) {
          border-radius: $radius-md;
          font-weight: 500;
        }
        
        :deep(.el-switch) {
          .el-switch__core {
            border-radius: 10px;
          }
          &.is-checked .el-switch__core {
            background: linear-gradient(135deg, $primary-color 0%, $primary-hover 100%);
          }
        }
      }
      
      // ============================================
      // Notification Items
      // ============================================
      .notification-group {
        margin-bottom: 28px;
        
        h4 {
          font-size: 15px;
          font-weight: 600;
          color: $text-primary;
          margin-bottom: 20px;
        }
        
        .notification-item {
          display: flex;
          align-items: center;
          justify-content: space-between;
          padding: 18px 0;
          border-bottom: 1px solid $border-color-light;
          
          .notification-info {
            .notification-label {
              display: block;
              font-size: 14px;
              color: $text-primary;
              margin-bottom: 4px;
              font-weight: 500;
            }
            
            .notification-desc {
              font-size: 13px;
              color: $text-muted;
            }
          }
          
          :deep(.el-switch) {
            .el-switch__core {
              border-radius: 10px;
            }
            &.is-checked .el-switch__core {
              background: linear-gradient(135deg, $primary-color 0%, $primary-hover 100%);
            }
          }
        }
      }
      
      // ============================================
      // Users Section
      // ============================================
      .users-header {
        display: flex;
        justify-content: space-between;
        margin-bottom: 20px;
        
        :deep(.el-input__wrapper) {
          background: $bg-secondary;
          border-radius: $radius-md;
          box-shadow: $shadow-inset;
          border: 1px solid $border-color-light;
        }
        
        :deep(.el-button--primary) {
          border-radius: $radius-md;
          font-weight: 600;
        }
      }
      
      :deep(.el-table) {
        background: transparent;
        
        th.el-table__cell {
          background: $bg-secondary;
          color: $text-secondary;
          font-weight: 600;
          font-size: 13px;
          border-bottom: 1px solid $border-color;
        }
        
        td.el-table__cell {
          color: $text-primary;
          border-bottom: 1px solid $border-color-light;
        }
        
        .el-table__row:hover > td.el-table__cell {
          background: rgba($primary-color, 0.04);
        }
        
        .el-tag {
          border-radius: $radius-full;
          padding: 4px 12px;
          font-weight: 500;
          border: none;
        }
      }
      
      // ============================================
      // Integration List
      // ============================================
      .integration-list {
        .integration-item {
          display: flex;
          align-items: center;
          padding: 24px;
          border: 1px solid $border-color;
          border-radius: $radius-lg;
          margin-bottom: 16px;
          transition: all $transition-base;
          background: $bg-secondary;
          
          &:hover {
            border-color: rgba($primary-color, 0.3);
            box-shadow: $shadow-sm;
          }
          
          .integration-icon {
            width: 52px;
            height: 52px;
            display: flex;
            align-items: center;
            justify-content: center;
            border-radius: $radius-md;
            margin-right: 20px;
            box-shadow: $shadow-sm;
            
            &.dingtalk {
              background: linear-gradient(135deg, #ecf5ff 0%, rgba($primary-color, 0.15) 100%);
              color: $primary-color;
            }
            
            &.wechat {
              background: linear-gradient(135deg, #f0f9eb 0%, rgba($success-color, 0.15) 100%);
              color: darken($success-color, 15%);
            }
            
            &.ldap {
              background: linear-gradient(135deg, #fdf6ec 0%, rgba($warning-color, 0.15) 100%);
              color: darken($warning-color, 15%);
            }
          }
          
          .integration-info {
            flex: 1;
            
            h4 {
              font-size: 15px;
              font-weight: 600;
              color: $text-primary;
              margin-bottom: 6px;
            }
            
            p {
              font-size: 13px;
              color: $text-muted;
            }
          }
          
          :deep(.el-button--primary) {
            border-radius: $radius-md;
            font-weight: 500;
            background: linear-gradient(135deg, $primary-color 0%, $primary-hover 100%);
            border: none;
            box-shadow: $shadow-sm;
          }
        }
      }
    }
  }
}
</style>

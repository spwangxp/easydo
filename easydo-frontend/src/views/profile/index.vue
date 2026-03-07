<template>
  <div class="profile-container">
    <div class="profile-header">
      <h1 class="page-title">个人中心</h1>
    </div>
    
    <div class="profile-layout">
      <aside class="profile-sidebar">
        <div class="user-card">
          <el-avatar :size="80" :src="userInfo.avatar">
            {{ userInfo.username?.charAt(0)?.toUpperCase() }}
          </el-avatar>
          <h3 class="username">{{ userInfo.username }}</h3>
          <p class="email">{{ userInfo.email }}</p>
        </div>
        
        <div class="profile-menu">
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
        </div>
      </aside>
      
      <main class="profile-content">
        <!-- 基本资料 -->
        <div v-if="activeMenu === 'profile'" class="profile-section">
          <h2 class="section-title">基本资料</h2>
          
          <el-form :model="profileForm" label-width="100px" class="profile-form">
            <el-form-item label="用户名">
              <el-input v-model="profileForm.username" disabled />
            </el-form-item>
            
            <el-form-item label="邮箱">
              <el-input v-model="profileForm.email" />
            </el-form-item>
            
            <el-form-item label="手机号">
              <el-input v-model="profileForm.phone" />
            </el-form-item>
            
            <el-form-item label="个人简介">
              <el-input 
                v-model="profileForm.bio" 
                type="textarea" 
                :rows="3"
                placeholder="请输入个人简介"
              />
            </el-form-item>
            
            <el-form-item>
              <el-button type="primary" @click="saveProfile">保存修改</el-button>
            </el-form-item>
          </el-form>
        </div>
        
        <!-- 安全设置 -->
        <div v-if="activeMenu === 'security'" class="profile-section">
          <h2 class="section-title">安全设置</h2>
          
          <div class="security-list">
            <div class="security-item">
              <div class="security-icon">
                <el-icon :size="24"><Lock /></el-icon>
              </div>
              <div class="security-content">
                <h4>登录密码</h4>
                <p>定期修改密码可以提高账户安全性</p>
              </div>
              <el-button @click="showPasswordDialog = true">修改密码</el-button>
            </div>
            
            <div class="security-item">
              <div class="security-icon">
                <el-icon :size="24"><Key /></el-icon>
              </div>
              <div class="security-content">
                <h4>API Token</h4>
                <p>用于API访问的认证Token</p>
              </div>
              <el-button @click="regenerateToken">重新生成</el-button>
            </div>
          </div>
        </div>
        
        <!-- 偏好设置 -->
        <div v-if="activeMenu === 'preferences'" class="profile-section">
          <h2 class="section-title">偏好设置</h2>
          
          <div class="preferences-form">
            <div class="form-item">
              <div class="form-label">
                <h4>语言</h4>
                <p>选择界面显示语言</p>
              </div>
              <el-select v-model="preferences.language" style="width: 200px">
                <el-option label="简体中文" value="zh-CN" />
                <el-option label="English" value="en-US" />
              </el-select>
            </div>
            
            <div class="form-item">
              <div class="form-label">
                <h4>时区</h4>
                <p>选择时区用于时间显示</p>
              </div>
              <el-select v-model="preferences.timezone" style="width: 200px">
                <el-option label="Asia/Shanghai (UTC+8)" value="Asia/Shanghai" />
                <el-option label="America/New_York (UTC-5)" value="America/New_York" />
                <el-option label="Europe/London (UTC+0)" value="Europe/London" />
              </el-select>
            </div>
            
            <div class="form-item">
              <div class="form-label">
                <h4>主题</h4>
                <p>选择界面主题颜色</p>
              </div>
              <el-radio-group v-model="preferences.theme">
                <el-radio-button label="light">浅色</el-radio-button>
                <el-radio-button label="dark">深色</el-radio-button>
              </el-radio-group>
            </div>
            
            <div class="form-item">
              <div class="form-label">
                <h4>通知方式</h4>
                <p>选择接收通知的方式</p>
              </div>
              <el-checkbox-group v-model="preferences.notificationMethods">
                <el-checkbox label="email">邮件</el-checkbox>
                <el-checkbox label="browser">浏览器推送</el-checkbox>
              </el-checkbox-group>
            </div>
            
            <div class="form-actions">
              <el-button type="primary" @click="savePreferences">保存设置</el-button>
            </div>
          </div>
        </div>
        
        <!-- 登录设备 -->
        <div v-if="activeMenu === 'devices'" class="profile-section">
          <h2 class="section-title">登录设备管理</h2>
          
          <div class="devices-list">
            <div 
              v-for="device in devices" 
              :key="device.id"
              class="device-item"
              :class="{ current: device.current }"
            >
              <div class="device-icon">
                <el-icon :size="32"><Monitor /></el-icon>
              </div>
              <div class="device-content">
                <div class="device-header">
                  <span class="device-name">{{ device.browser }} on {{ device.os }}</span>
                  <el-tag v-if="device.current" type="success" size="small">当前设备</el-tag>
                </div>
                <p class="device-info">{{ device.ip }} · {{ device.location }}</p>
                <p class="device-time">最后活跃: {{ device.lastActive }}</p>
              </div>
              <el-button 
                v-if="!device.current" 
                type="danger" 
                size="small"
                @click="logoutDevice(device.id)"
              >
                退出登录
              </el-button>
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
  User, 
  Lock, 
  Setting, 
  Monitor,
  Key
} from '@element-plus/icons-vue'

const activeMenu = ref('profile')

const menuItems = [
  { key: 'profile', name: '基本资料', icon: User },
  { key: 'security', name: '安全设置', icon: Lock },
  { key: 'preferences', name: '偏好设置', icon: Setting },
  { key: 'devices', name: '登录设备', icon: Monitor }
]

const userInfo = reactive({
  username: 'demo',
  email: 'demo@example.com',
  avatar: ''
})

const profileForm = reactive({
  username: 'demo',
  email: 'demo@example.com',
  phone: '',
  bio: ''
})

const preferences = reactive({
  language: 'zh-CN',
  timezone: 'Asia/Shanghai',
  theme: 'light',
  notificationMethods: ['email', 'browser']
})

const devices = ref([
  {
    id: 1,
    browser: 'Chrome 144',
    os: 'Mac OS X',
    ip: '192.168.1.100',
    location: 'Shanghai, China',
    lastActive: '刚刚',
    current: true
  },
  {
    id: 2,
    browser: 'Edge 143',
    os: 'Windows 11',
    ip: '192.168.1.101',
    location: 'Shanghai, China',
    lastActive: '2 小时前',
    current: false
  }
])

const showPasswordDialog = ref(false)

const saveProfile = () => {
  console.log('保存资料')
}

const savePreferences = () => {
  console.log('保存偏好设置')
}

const regenerateToken = () => {
  console.log('重新生成Token')
}

const logoutDevice = (deviceId) => {
  devices.value = devices.value.filter(d => d.id !== deviceId)
}
</script>

<style lang="scss" scoped>
.profile-container {
  .profile-header {
    margin-bottom: 20px;
    
    .page-title {
      font-size: 24px;
      font-weight: 600;
      color: var(--text-primary);
    }
  }
  
  .profile-layout {
    display: flex;
    gap: 20px;
    
    .profile-sidebar {
      width: 280px;
      flex-shrink: 0;
      
      .user-card {
        background: var(--bg-card);
        border-radius: 8px;
        padding: 30px 20px;
        text-align: center;
        margin-bottom: 16px;
        
        .el-avatar {
          background: #409EFF;
          font-size: 28px;
          margin-bottom: 16px;
        }
        
        .username {
          font-size: 18px;
          font-weight: 500;
          color: var(--text-primary);
          margin-bottom: 8px;
        }
        
        .email {
          font-size: 13px;
          color: var(--text-muted);
        }
      }
      
      .profile-menu {
        background: var(--bg-card);
        border-radius: 8px;
        padding: 12px;
        
        .menu-item {
          display: flex;
          align-items: center;
          gap: 12px;
          padding: 12px 16px;
          color: var(--text-secondary);
          cursor: pointer;
          border-radius: 6px;
          transition: all 0.3s;
          
          &:hover {
            background: var(--bg-secondary);
          }
          
          &.active {
            color: var(--primary-color);
            background: var(--primary-lighter);
          }
        }
      }
    }
    
    .profile-content {
      flex: 1;
      background: var(--bg-card);
      border-radius: 8px;
      padding: 24px;
      
      .profile-section {
        .section-title {
          font-size: 18px;
          font-weight: 500;
          color: var(--text-primary);
          margin-bottom: 24px;
          padding-bottom: 16px;
          border-bottom: 1px solid #ebeef5;
        }
      }
      
      .profile-form {
        max-width: 500px;
      }
      
      .security-list {
        .security-item {
          display: flex;
          align-items: center;
          padding: 20px;
          border: 1px solid #ebeef5;
          border-radius: 8px;
          margin-bottom: 12px;
          
          .security-icon {
            width: 48px;
            height: 48px;
            display: flex;
            align-items: center;
            justify-content: center;
            background: var(--primary-lighter);
            color: var(--primary-color);
            border-radius: 8px;
            margin-right: 16px;
          }
          
          .security-content {
            flex: 1;
            
            h4 {
              font-size: 14px;
              font-weight: 500;
              color: var(--text-primary);
              margin-bottom: 4px;
            }
            
            p {
              font-size: 12px;
              color: var(--text-muted);
            }
          }
        }
      }
      
      .preferences-form {
        .form-item {
          display: flex;
          align-items: center;
          justify-content: space-between;
          padding: 20px 0;
          border-bottom: 1px solid #ebeef5;
          
          .form-label {
            h4 {
              font-size: 14px;
              font-weight: 500;
              color: var(--text-primary);
              margin-bottom: 4px;
            }
            
            p {
              font-size: 12px;
              color: var(--text-muted);
            }
          }
        }
        
        .form-actions {
          margin-top: 24px;
        }
      }
      
      .devices-list {
        .device-item {
          display: flex;
          align-items: center;
          padding: 20px;
          border: 1px solid #ebeef5;
          border-radius: 8px;
          margin-bottom: 12px;
          
          &.current {
            background: var(--success-light);
            border-color: var(--success-color);
          }
          
          .device-icon {
            width: 56px;
            height: 56px;
            display: flex;
            align-items: center;
            justify-content: center;
            background: var(--bg-secondary);
            border-radius: 8px;
            margin-right: 16px;
            color: var(--text-secondary);
          }
          
          .device-content {
            flex: 1;
            
            .device-header {
              display: flex;
              align-items: center;
              gap: 12px;
              margin-bottom: 4px;
              
              .device-name {
                font-size: 14px;
                font-weight: 500;
                color: var(--text-primary);
              }
            }
            
            .device-info,
            .device-time {
              font-size: 12px;
              color: var(--text-muted);
              margin-top: 4px;
            }
          }
        }
      }
    }
  }
}
</style>

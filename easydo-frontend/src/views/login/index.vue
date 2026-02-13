<template>
  <div class="login-container">
    <!-- Blur background shapes -->
    <div class="blur-shape-1"></div>
    <div class="blur-shape-2"></div>
    <div class="blur-shape-3"></div>
    
    <!-- Left side - decorative -->
    <div class="login-left">
      <div class="brand-section">
        <img src="@/assets/images/logo.svg" alt="Logo" class="brand-logo" />
        <h1 class="brand-name">EasyDo</h1>
        <p class="brand-tagline">智能化 DevOps 工作平台</p>
      </div>
      
      <div class="features">
        <div class="feature-item">
          <div class="feature-icon">
            <el-icon :size="24"><Connection /></el-icon>
          </div>
          <span class="feature-text">流水线</span>
        </div>
        <div class="feature-item">
          <div class="feature-icon">
            <el-icon :size="24"><Box /></el-icon>
          </div>
          <span class="feature-text">项目管理</span>
        </div>
        <div class="feature-item">
          <div class="feature-icon">
            <el-icon :size="24"><Monitor /></el-icon>
          </div>
          <span class="feature-text">执行器</span>
        </div>
      </div>
    </div>
    
    <!-- Right side - login form -->
    <div class="login-right">
      <div class="login-box">
        <div class="login-header">
          <img src="@/assets/images/logo.svg" alt="Logo" class="logo" />
          <span class="title">EasyDo</span>
        </div>
        
        <div class="login-form">
          <h2 class="form-title">欢迎回来</h2>
          
          <el-form
            ref="loginFormRef"
            :model="loginForm"
            :rules="loginRules"
            label-width="0"
          >
            <el-form-item prop="username">
              <el-input
                v-model="loginForm.username"
                placeholder="请输入邮箱或手机号码、用户名"
                :prefix-icon="User"
                size="large"
                @keyup.enter="handleLogin"
              />
            </el-form-item>
            
            <el-form-item prop="password">
              <el-input
                v-model="loginForm.password"
                :type="passwordVisible ? 'text' : 'password'"
                placeholder="请输入密码"
                :prefix-icon="Lock"
                size="large"
                @keyup.enter="handleLogin"
              >
                <template #suffix>
                  <el-icon 
                    class="password-toggle" 
                    @click="passwordVisible = !passwordVisible"
                  >
                    <component :is="passwordVisible ? 'View' : 'Hide'" />
                  </el-icon>
                </template>
              </el-input>
            </el-form-item>
            
            <el-form-item>
              <div class="form-options">
                <el-checkbox v-model="loginForm.remember">记住登录状态</el-checkbox>
                <span class="forgot-password">忘记密码</span>
              </div>
            </el-form-item>
            
            <el-form-item>
              <el-button
                type="primary"
                size="large"
                class="login-button"
                :loading="loading"
                @click="handleLogin"
              >
                登 录
              </el-button>
            </el-form-item>
          </el-form>
          
          <div class="other-login">
            <div class="divider">
              <span>其他登录方式</span>
            </div>
            <div class="social-login">
              <el-tooltip content="钉钉" placement="top">
                <div class="social-icon">
                  <img src="@/assets/images/dingtalk.svg" alt="钉钉" />
                </div>
              </el-tooltip>
              <el-tooltip content="企业微信" placement="top">
                <div class="social-icon">
                  <img src="@/assets/images/wechat-work.svg" alt="企业微信" />
                </div>
              </el-tooltip>
              <el-tooltip content="LDAP" placement="top">
                <div class="social-icon">
                  <img src="@/assets/images/ldap.svg" alt="LDAP" />
                </div>
              </el-tooltip>
            </div>
          </div>
        </div>
        
        <div class="login-footer">
          <span>© 2024-2025 EasyDo 版权所有</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { User, Lock, View, Hide, Connection, Box, Monitor } from '@element-plus/icons-vue'
import { useUserStore } from '@/stores/user'

const router = useRouter()
const route = useRoute()
const userStore = useUserStore()

const loginFormRef = ref(null)
const loading = ref(false)
const passwordVisible = ref(false)

const loginForm = reactive({
  username: '',
  password: '',
  remember: true
})

const loginRules = {
  username: [
    { required: true, message: '请输入用户名', trigger: 'blur' }
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' }
  ]
}

const handleLogin = async () => {
  if (!loginForm.username || !loginForm.password) {
    ElMessage.error('请输入用户名和密码')
    return
  }

  loading.value = true
  try {
    const result = await userStore.doLogin(loginForm.username, loginForm.password)

    if (result.success) {
      ElMessage.success('登录成功')
      const redirect = route.query.redirect || '/'
      router.push(redirect)
    } else {
      ElMessage.error(result.message || '登录失败')
    }
  } catch (error) {
    ElMessage.error('登录失败，请稍后重试')
  } finally {
    loading.value = false
  }
}
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.login-container {
  width: 100%;
  height: 100vh;
  display: flex;
  position: relative;
  overflow: hidden;
  background: linear-gradient(135deg, var(--bg-primary) 0%, var(--bg-secondary) 50%, var(--primary-light) 100%);
  
  // Blur background shapes for depth
  .blur-shape-1 {
    position: absolute;
    width: 600px;
    height: 600px;
    border-radius: 50%;
    background: linear-gradient(135deg, rgba($primary-color, 0.2) 0%, rgba($success-color, 0.15) 100%);
    filter: blur(80px);
    top: -200px;
    left: -200px;
    animation: float 20s ease-in-out infinite;
  }
  
  .blur-shape-2 {
    position: absolute;
    width: 500px;
    height: 500px;
    border-radius: 50%;
    background: linear-gradient(135deg, rgba($warning-color, 0.15) 0%, rgba($primary-color, 0.1) 100%);
    filter: blur(60px);
    bottom: -150px;
    right: -100px;
    animation: float 25s ease-in-out infinite reverse;
  }
  
  .blur-shape-3 {
    position: absolute;
    width: 300px;
    height: 300px;
    border-radius: 50%;
    background: rgba($info-color, 0.12);
    filter: blur(50px);
    top: 50%;
    left: 30%;
    animation: float 18s ease-in-out infinite;
  }
}

@keyframes float {
  0%, 100% { transform: translate(0, 0) scale(1); }
  33% { transform: translate(30px, -30px) scale(1.05); }
  66% { transform: translate(-20px, 20px) scale(0.95); }
}

// Left side - decorative
.login-left {
  flex: 1;
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  padding: 60px;
  position: relative;
  z-index: 1;
  
  .brand-section {
    text-align: center;
    
    .brand-logo {
      width: 80px;
      height: 80px;
      margin-bottom: 24px;
      filter: drop-shadow(0 8px 24px rgba($primary-color, 0.3));
    }
    
    .brand-name {
      font-family: $font-family-display;
      font-size: 48px;
      font-weight: 700;
      color: var(--text-primary);
      margin-bottom: 16px;
      letter-spacing: -0.03em;
    }
    
    .brand-tagline {
      font-size: 18px;
      color: var(--text-secondary);
      font-weight: 400;
    }
  }
  
  .features {
    display: flex;
    gap: 40px;
    margin-top: 60px;
    
    .feature-item {
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 12px;
      
      .feature-icon {
        width: 56px;
        height: 56px;
        border-radius: $radius-lg;
        background: var(--bg-card);
        box-shadow: $shadow-md;
        display: flex;
        align-items: center;
        justify-content: center;
        color: var(--primary-color);
        font-size: 24px;
      }
      
      .feature-text {
        font-size: 14px;
        color: var(--text-secondary);
        font-weight: 500;
      }
    }
  }
}

// Right side - login form
.login-right {
  width: 480px;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 40px;
  position: relative;
  z-index: 1;
}

.login-box {
  width: 100%;
  max-width: 400px;
  background: $glass-bg;
  backdrop-filter: $blur-lg;
  -webkit-backdrop-filter: $blur-lg;
  border-radius: $radius-2xl;
  padding: 48px;
  box-shadow: 
    0 8px 32px rgba(0, 0, 0, 0.06),
    inset 0 0 0 1px rgba(255, 255, 255, 0.6);
  border: 1px solid rgba(255, 255, 255, 0.4);
}

.login-header {
  text-align: center;
  margin-bottom: 32px;
  
  .logo {
    width: 56px;
    height: 56px;
    margin-bottom: 16px;
    filter: drop-shadow(0 4px 12px rgba($primary-color, 0.25));
  }
  
  .title {
    font-family: $font-family-display;
    font-size: 28px;
    font-weight: 700;
    color: var(--text-primary);
    letter-spacing: -0.02em;
  }
}

.login-form {
  .form-title {
    font-size: 16px;
    color: var(--text-secondary);
    text-align: center;
    margin-bottom: 28px;
    font-weight: 400;
  }
  
  :deep(.el-form-item) {
    margin-bottom: 20px;
  }
  
  :deep(.el-input__wrapper) {
    background: var(--bg-secondary);
    border-radius: $radius-md;
    box-shadow: $shadow-inset;
    border: 1px solid var(--border-color-light);
    padding: 12px 16px;
    transition: all $transition-base;
    
    &:hover, &.is-focus {
      border-color: rgba($primary-color, 0.4);
      box-shadow: $shadow-inset, 0 0 0 3px rgba($primary-color, 0.08);
    }
    
    .el-input__inner {
      color: var(--text-primary);
      font-family: $font-family-body;
      font-size: 15px;
      
      &::placeholder {
        color: var(--text-placeholder);
      }
    }
    
    .el-input__icon {
      color: var(--text-tertiary);
    }
  }
  
  .password-toggle {
    cursor: pointer;
    color: var(--text-tertiary);
    transition: color $transition-fast;
    
    &:hover {
      color: var(--primary-color);
    }
  }
  
  .form-options {
    width: 100%;
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 8px;
    
    :deep(.el-checkbox__label) {
      color: var(--text-secondary);
      font-size: 13px;
    }
    
    :deep(.el-checkbox__input.is-checked .el-checkbox__inner) {
      background-color: var(--primary-color);
      border-color: var(--primary-color);
    }
    
    .forgot-password {
      color: var(--primary-color);
      font-size: 13px;
      cursor: pointer;
      transition: color $transition-fast;
      
      &:hover {
        color: $primary-active;
        text-decoration: underline;
      }
    }
  }
  
  .login-button {
    width: 100%;
    height: 48px;
    font-size: 16px;
    font-weight: 600;
    border-radius: $radius-md;
    background: linear-gradient(135deg, $primary-color 0%, $primary-hover 100%);
    border: none;
    box-shadow: $shadow-md;
    transition: all $transition-base;
    margin-top: 8px;
    
    &:hover {
      transform: translateY(-2px);
      box-shadow: $shadow-lg;
      background: linear-gradient(135deg, $primary-hover 0%, $primary-color 100%);
    }
    
    &:active {
      transform: translateY(0);
      box-shadow: $shadow-inset;
    }
  }
}

.other-login {
  margin-top: 32px;
  text-align: center;
  
  .divider {
    display: flex;
    align-items: center;
    gap: 16px;
    margin-bottom: 24px;
    
    &::before,
    &::after {
      content: '';
      flex: 1;
      height: 1px;
      background: linear-gradient(90deg, transparent, $border-color-medium, transparent);
    }
    
    span {
      color: var(--text-muted);
      font-size: 13px;
    }
  }
  
  .social-login {
    display: flex;
    justify-content: center;
    gap: 16px;
    
    .social-icon {
      width: 48px;
      height: 48px;
      border-radius: $radius-lg;
      cursor: pointer;
      display: flex;
      align-items: center;
      justify-content: center;
      background: var(--bg-secondary);
      box-shadow: $shadow-sm;
      transition: all $transition-base;
      
      &:hover {
        transform: translateY(-2px);
        box-shadow: $shadow-md;
        background: var(--bg-card);
      }
      
      &:active {
        transform: translateY(0);
        box-shadow: $shadow-inset;
      }
      
      img {
        width: 22px;
        height: 22px;
        opacity: 0.7;
        transition: opacity $transition-fast;
      }
      
      &:hover img {
        opacity: 1;
      }
    }
  }
}

.login-footer {
  margin-top: 32px;
  text-align: center;
  color: var(--text-muted);
  font-size: 12px;
}

// Responsive
@media (max-width: 992px) {
  .login-left {
    display: none;
  }
  
  .login-right {
    width: 100%;
  }
}
</style>

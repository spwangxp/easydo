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
  position: relative;
  width: 100%;
  min-height: 100vh;
  display: flex;
  overflow: hidden;
  background:
    radial-gradient(circle at 12% 22%, rgba($primary-color, 0.28), transparent 42%),
    radial-gradient(circle at 88% 12%, rgba($info-color, 0.22), transparent 36%),
    radial-gradient(circle at 72% 86%, rgba($primary-color, 0.14), transparent 46%),
    linear-gradient(150deg, #eaf2ff 0%, #f4f8ff 46%, #edf5ff 100%);

  .blur-shape-1,
  .blur-shape-2,
  .blur-shape-3 {
    position: absolute;
    border-radius: 50%;
    pointer-events: none;
    filter: blur(72px);
    animation: floating 20s ease-in-out infinite;
  }

  .blur-shape-1 {
    width: 420px;
    height: 420px;
    left: -120px;
    top: -120px;
    background: rgba($primary-color, 0.26);
  }

  .blur-shape-2 {
    width: 320px;
    height: 320px;
    right: -80px;
    top: 8%;
    background: rgba($info-color, 0.24);
    animation-delay: -6s;
  }

  .blur-shape-3 {
    width: 300px;
    height: 300px;
    right: 16%;
    bottom: -90px;
    background: rgba($warning-color, 0.2);
    animation-delay: -11s;
  }
}

@keyframes floating {
  0%,
  100% { transform: translate3d(0, 0, 0); }
  50% { transform: translate3d(-14px, 16px, 0); }
}

.login-left {
  flex: 1;
  display: flex;
  flex-direction: column;
  justify-content: center;
  padding: 56px 72px;
  position: relative;
  z-index: 1;

  .brand-section {
    max-width: 560px;

    .brand-logo {
      width: 82px;
      height: 82px;
      margin-bottom: 18px;
      filter: drop-shadow(0 10px 24px rgba($primary-color, 0.26));
    }

    .brand-name {
      font-family: $font-family-display;
      font-size: 56px;
      line-height: 1;
      letter-spacing: -0.04em;
      color: var(--text-primary);
      font-weight: 800;
      margin-bottom: 14px;
    }

    .brand-tagline {
      font-size: 19px;
      color: var(--text-secondary);
      font-weight: 500;
      letter-spacing: 0.01em;
    }
  }

  .features {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 14px;
    margin-top: 56px;
    max-width: 620px;

    .feature-item {
      border-radius: $radius-xl;
      border: 1px solid rgba(255, 255, 255, 0.55);
      background: rgba(255, 255, 255, 0.58);
      backdrop-filter: $blur-sm;
      -webkit-backdrop-filter: $blur-sm;
      box-shadow: var(--shadow-sm);
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 10px;
      padding: 18px 14px;
      transition: transform $transition-base, box-shadow $transition-base;

      &:hover {
        transform: translateY(-3px);
        box-shadow: var(--shadow-md);
      }

      .feature-icon {
        width: 50px;
        height: 50px;
        border-radius: 14px;
        display: flex;
        align-items: center;
        justify-content: center;
        color: var(--primary-color);
        background: linear-gradient(140deg, rgba($primary-color, 0.22), rgba($primary-color, 0.08));
      }

      .feature-text {
        font-size: 13px;
        color: var(--text-secondary);
        font-weight: 600;
      }
    }
  }
}

.login-right {
  width: 520px;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 42px;
  position: relative;
  z-index: 1;
}

.login-box {
  width: 100%;
  max-width: 420px;
  padding: 40px 34px 30px;
  border-radius: 28px;
  border: 1px solid rgba(255, 255, 255, 0.64);
  background: rgba(255, 255, 255, 0.7);
  backdrop-filter: $blur-lg;
  -webkit-backdrop-filter: $blur-lg;
  box-shadow: var(--shadow-xl);
  animation: float-up 0.52s ease both;
}

.login-header {
  text-align: center;
  margin-bottom: 26px;

  .logo {
    width: 58px;
    height: 58px;
    margin-bottom: 12px;
    filter: drop-shadow(0 6px 16px rgba($primary-color, 0.24));
  }

  .title {
    font-family: $font-family-display;
    font-size: 30px;
    letter-spacing: -0.03em;
    font-weight: 760;
    color: var(--text-primary);
  }
}

.login-form {
  .form-title {
    margin-bottom: 20px;
    text-align: center;
    color: var(--text-secondary);
    font-size: 15px;
    font-weight: 500;
  }

  :deep(.el-form-item) {
    margin-bottom: 16px;
  }

  :deep(.el-input__wrapper) {
    min-height: 46px;
    border-radius: 14px;
    background: rgba(255, 255, 255, 0.8);

    .el-input__inner {
      font-size: 14px;
    }

    .el-input__icon {
      color: var(--text-tertiary);
    }
  }

  .password-toggle {
    cursor: pointer;
    color: var(--text-tertiary);

    &:hover {
      color: var(--primary-color);
    }
  }

  .form-options {
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin: 2px 0 4px;

    :deep(.el-checkbox__label) {
      color: var(--text-secondary);
      font-size: 13px;
    }

    .forgot-password {
      color: var(--primary-color);
      font-size: 13px;
      font-weight: 600;
      cursor: pointer;

      &:hover {
        text-decoration: underline;
      }
    }
  }

  .login-button {
    width: 100%;
    height: 46px;
    margin-top: 4px;
    border-radius: 14px;
    font-size: 15px;
    letter-spacing: 0.06em;
    font-weight: 700;
  }
}

.other-login {
  margin-top: 24px;
  text-align: center;

  .divider {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 16px;

    &::before,
    &::after {
      content: '';
      flex: 1;
      height: 1px;
      background: linear-gradient(90deg, transparent, var(--border-color-medium), transparent);
    }

    span {
      color: var(--text-muted);
      font-size: 12px;
      font-weight: 500;
    }
  }

  .social-login {
    display: flex;
    justify-content: center;
    gap: 12px;

    .social-icon {
      width: 44px;
      height: 44px;
      border-radius: 14px;
      border: 1px solid var(--border-color-light);
      background: rgba(255, 255, 255, 0.76);
      display: flex;
      align-items: center;
      justify-content: center;
      cursor: pointer;
      transition: all $transition-fast;

      &:hover {
        transform: translateY(-2px);
        border-color: var(--border-color-hover);
        box-shadow: var(--shadow-md);
      }

      img {
        width: 22px;
        height: 22px;
        opacity: 0.72;
      }

      &:hover img {
        opacity: 1;
      }
    }
  }
}

.login-footer {
  margin-top: 24px;
  text-align: center;
  color: var(--text-muted);
  font-size: 12px;
}

@media (max-width: 1200px) {
  .login-left {
    padding: 44px;

    .brand-section {
      .brand-name {
        font-size: 46px;
      }

      .brand-tagline {
        font-size: 16px;
      }
    }
  }

  .login-right {
    width: 460px;
    padding: 24px;
  }
}

@media (max-width: 992px) {
  .login-left {
    display: none;
  }

  .login-right {
    width: 100%;
    padding: 20px;
  }

  .login-box {
    max-width: 420px;
    padding: 34px 24px 26px;
  }
}
</style>

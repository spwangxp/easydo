import { createRouter, createWebHistory } from 'vue-router'
import { useUserStore } from '@/stores/user'

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/login/index.vue'),
    meta: { requiresAuth: false }
  },
  {
    path: '',
    component: () => import('@/views/layout/index.vue'),
    meta: { requiresAuth: true },
    children: [
      {
        path: '',
        name: 'Dashboard',
        component: () => import('@/views/dashboard/index.vue')
      },
      {
        path: 'pipeline',
        name: 'Pipeline',
        component: () => import('@/views/pipeline/index.vue')
      },
      {
        path: 'pipeline/:id',
        name: 'PipelineDetail',
        component: () => import('@/views/pipeline/detail.vue')
      },
      {
        path: 'project',
        name: 'Project',
        component: () => import('@/views/project/index.vue')
      },
      {
        path: 'deploy',
        name: 'Deploy',
        component: () => import('@/views/deploy/index.vue')
      },
      {
        path: 'statistics',
        name: 'Statistics',
        component: () => import('@/views/statistics/index.vue')
      },
      {
        path: 'settings',
        name: 'Settings',
        component: () => import('@/views/settings/index.vue')
      },
      {
        path: 'messages',
        name: 'Messages',
        component: () => import('@/views/messages/index.vue')
      },
      {
        path: 'profile',
        name: 'Profile',
        component: () => import('@/views/profile/index.vue')
      },
      {
        path: 'agent',
        name: 'Agent',
        component: () => import('@/views/agent/index.vue')
      },
      {
        path: 'agent/pending',
        name: 'AgentPending',
        component: () => import('@/views/agent/pending.vue')
      },
      {
        path: 'secrets',
        name: 'Secrets',
        component: () => import('@/views/secrets/index.vue')
      },
      {
        path: 'credentials',
        name: 'Credentials',
        component: () => import('@/views/credential/index.vue')
      }
    ]
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/'
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

// 路由守卫
router.beforeEach((to, from, next) => {
  // 延迟导入store，避免循环依赖
  import('@/stores/user').then(({ useUserStore }) => {
    const userStore = useUserStore()
    
    if (to.meta.requiresAuth && !userStore.isLoggedIn) {
      next({ name: 'Login', query: { redirect: to.fullPath } })
    } else if (to.name === 'Login' && userStore.isLoggedIn) {
      next({ name: 'Dashboard' })
    } else {
      next()
    }
  }).catch(err => {
    console.error('Route guard error:', err)
    next()
  })
})

export default router

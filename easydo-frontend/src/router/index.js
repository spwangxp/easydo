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
    path: '/workspace-invitations/:token',
    name: 'WorkspaceInvitation',
    component: () => import('@/views/workspace-invitation/index.vue'),
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
        component: () => import('@/views/pipeline/index.vue'),
        meta: { permission: 'pipeline.read' }
      },
      {
        path: 'pipeline/:id',
        name: 'PipelineDetail',
        component: () => import('@/views/pipeline/detail.vue')
      },
      {
        path: 'project',
        name: 'Project',
        component: () => import('@/views/project/index.vue'),
        meta: { permission: 'project.read' }
      },
      {
        path: 'deploy',
        name: 'Deploy',
        component: () => import('@/views/deploy/index.vue')
      },
      {
        path: 'statistics',
        name: 'Statistics',
        component: () => import('@/views/statistics/index.vue'),
        meta: { permission: 'workspace.read' }
      },
      {
        path: 'settings',
        name: 'Settings',
        component: () => import('@/views/settings/index.vue'),
        meta: { permission: 'workspace.read' }
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
        component: () => import('@/views/agent/index.vue'),
        meta: { permission: 'agent.read' }
      },
      {
        path: 'agent/pending',
        name: 'AgentPending',
        component: () => import('@/views/agent/pending.vue'),
        meta: { permission: 'agent.approve' }
      },
      {
        path: 'secrets',
        name: 'Secrets',
        component: () => import('@/views/secrets/index.vue'),
        meta: { permission: 'credential.read' }
      },
      {
        path: 'credentials',
        redirect: '/secrets'
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
  import('@/stores/user').then(async ({ useUserStore }) => {
    const userStore = useUserStore()
    if (userStore.isLoggedIn && !userStore.userInfo?.id) {
      await userStore.getUserInfoAction()
    }
    
    if (to.meta.requiresAuth && !userStore.isLoggedIn) {
      next({ name: 'Login', query: { redirect: to.fullPath } })
    } else if (to.name === 'Login' && userStore.isLoggedIn) {
      next({ name: 'Dashboard' })
    } else if (to.meta.permission && !userStore.hasPermission(to.meta.permission)) {
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

import { createRouter, createWebHistory } from 'vue-router'
import { TOKEN_KEY } from '../api/client'

const router = createRouter({
  history: createWebHistory('/admin/'),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('../views/LoginView.vue'),
      meta: { public: true },
    },
    {
      path: '/',
      component: () => import('../layouts/AdminLayout.vue'),
      children: [
        { path: '', redirect: '/dashboard' },
        { path: 'dashboard', name: 'dashboard', component: () => import('../views/DashboardView.vue'), meta: { title: '概览' } },
        { path: 'users', name: 'users', component: () => import('../views/UsersView.vue'), meta: { title: '用户管理' } },
        { path: 'families', name: 'families', component: () => import('../views/FamiliesView.vue'), meta: { title: '家庭与账目' } },
        { path: 'audit', name: 'audit', component: () => import('../views/AuditView.vue'), meta: { title: '审计日志' } },
        { path: 'devices', name: 'devices', component: () => import('../views/DevicesView.vue'), meta: { title: '设备授权' } },
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/dashboard' },
  ],
})

router.beforeEach((to) => {
  const token = localStorage.getItem(TOKEN_KEY)
  if (!to.meta.public && !token) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  if (to.path === '/login' && token) {
    return '/dashboard'
  }
  return true
})

export default router

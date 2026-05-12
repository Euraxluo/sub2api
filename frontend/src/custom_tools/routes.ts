import type { RouteRecordRaw } from 'vue-router'

const customToolsRoutes: RouteRecordRaw[] = [
  {
    path: '/custom/admin-tools',
    name: 'AdminTools',
    component: () => import('./AdminTools.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Admin Tools',
      titleKey: 'admin.tools.title'
    }
  }
]

export default customToolsRoutes

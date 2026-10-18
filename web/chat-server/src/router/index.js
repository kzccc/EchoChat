// 路由表负责声明页面映射关系，并在进入受保护页面前校验登录态。
// meta.shell 为 true 的页面会套上"左导航 + 列表 + 内容"的微信式外壳。
import { createRouter, createWebHistory } from 'vue-router'
import store from '../store/index.js'

const routes = [
  {
    path: '/',
    redirect: { name: 'Login' }
  },
  {
    path: '/login',
    name: 'Login',
    meta: { shell: false, public: true },
    component: () => import('../views/access/Login.vue')
  },
  {
    path: '/smsLogin',
    name: 'SmsLogin',
    meta: { shell: false, public: true },
    component: () => import('../views/access/SmsLogin.vue')
  },
  {
    path: '/register',
    name: 'Register',
    meta: { shell: false, public: true },
    component: () => import('../views/access/Register.vue')
  },
  // 静态路径必须放在 /chat/:id 之前声明，避免被动态段吃掉。
  {
    path: '/chat/sessionList',
    name: 'SessionList',
    meta: { shell: true },
    component: () => import('../views/chat/session/SessionList.vue')
  },
  {
    path: '/chat/contactlist',
    name: 'ContactList',
    meta: { shell: true },
    component: () => import('../views/chat/contact/ContactList.vue')
  },
  {
    path: '/chat/owninfo',
    name: 'OwnInfo',
    meta: { shell: true },
    component: () => import('../views/chat/user/OwnInfo.vue')
  },
  {
    path: '/chat/:id',
    name: 'ContactChat',
    meta: { shell: true },
    component: () => import('../views/chat/contact/ContactChat.vue')
  },
  {
    path: '/manager',
    name: 'Manager',
    meta: { shell: true, admin: true },
    component: () => import('../views/manager/Manager.vue')
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: { name: 'Login' }
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

router.beforeEach((to, from, next) => {
  const hasAuth = !!store.state.accessToken && !!store.state.refreshToken && !!store.state.userInfo.uuid
  // 未登录时只允许访问登录、注册和验证码登录页，其余页面统一跳回登录页。
  if (!hasAuth) {
    if (to.meta?.public) {
      next()
      return
    }
    next('/login')
    return
  }
  if (to.meta?.public) {
    next('/chat/sessionList')
    return
  }
  if (to.meta?.admin && Number(store.state.userInfo.is_admin) !== 1) {
    next('/chat/sessionList')
    return
  }
  next()
})

export default router

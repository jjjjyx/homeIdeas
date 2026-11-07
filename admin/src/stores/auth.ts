import { defineStore } from 'pinia'
import { apiGet, apiPost, TOKEN_KEY, USER_KEY } from '../api/client'

interface AdminUser {
  id: string
  account: string
  nickname: string
  role?: string
}

function readUser(): AdminUser | null {
  try {
    const raw = localStorage.getItem(USER_KEY)
    return raw ? (JSON.parse(raw) as AdminUser) : null
  } catch {
    return null
  }
}

export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: localStorage.getItem(TOKEN_KEY) || '',
    user: readUser(),
  }),
  getters: {
    isLoggedIn: (s) => !!s.token,
    displayName: (s) => s.user?.nickname || s.user?.account || '管理员',
  },
  actions: {
    async login(account: string, password: string) {
      const data: any = await apiPost('/auth/login', { account, password })
      this.token = data.access_token
      this.user = data.user
      localStorage.setItem(TOKEN_KEY, data.access_token)
      localStorage.setItem(USER_KEY, JSON.stringify(data.user))
    },
    async refreshMe() {
      const data: any = await apiGet('/me')
      this.user = data.user
      localStorage.setItem(USER_KEY, JSON.stringify(data.user))
    },
    logout() {
      this.token = ''
      this.user = null
      localStorage.removeItem(TOKEN_KEY)
      localStorage.removeItem(USER_KEY)
    },
  },
})

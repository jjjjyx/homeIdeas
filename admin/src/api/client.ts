import axios from 'axios'

export const TOKEN_KEY = 'admin_token'
export const USER_KEY = 'admin_user'

export const client = axios.create({
  baseURL: '/admin/api',
  timeout: 15000,
})

client.interceptors.request.use((config) => {
  const token = localStorage.getItem(TOKEN_KEY)
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

client.interceptors.response.use(
  (res) => res.data,
  (error) => {
    const status = error.response?.status
    if (status === 401) {
      localStorage.removeItem(TOKEN_KEY)
      if (!location.pathname.endsWith('/login')) {
        location.href = '/admin/login'
      }
    }
    const message =
      error.response?.data?.message || error.message || '网络异常，请稍后重试'
    return Promise.reject(new Error(message))
  },
)

// 后端统一返回 { code, message, data }，这里只把 data 交给页面
export async function apiGet(url: string, params?: Record<string, unknown>) {
  const res: any = await client.get(url, { params })
  return res.data
}

export async function apiPost(url: string, body?: unknown) {
  const res: any = await client.post(url, body ?? {})
  return res.data
}

export async function apiPatch(url: string, body?: unknown) {
  const res: any = await client.patch(url, body ?? {})
  return res.data
}

export function formatTime(ms?: number | null) {
  if (!ms) return '-'
  const d = new Date(ms)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

export function formatMoney(cents?: number | null) {
  if (!cents) return '0.00'
  return (cents / 100).toLocaleString('zh-CN', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
}

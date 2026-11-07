<template>
  <div>
    <el-card shadow="never" class="block">
      <template #header>
        <div class="head">
          <span>待授权设备</span>
          <div>
            <el-tag v-if="pending.length" type="warning" size="small" class="mr">
              {{ pending.length }} 台等待批准
            </el-tag>
            <el-button text :loading="loading" @click="loadAll">刷新</el-button>
          </div>
        </div>
      </template>

      <el-empty v-if="!pending.length" description="没有待授权的设备" :image-size="80" />

      <el-table v-else :data="pending" v-loading="loading" stripe>
        <el-table-column prop="model" label="设备" min-width="150">
          <template #default="{ row }">
            {{ row.model || '未知机型' }}
            <el-tooltip v-if="row.last_account" placement="top">
              <template #content>
                这台手机上次授权给了「{{ row.last_nickname || row.last_account }}」<br />
                {{ formatTime(row.last_approved_at) }}<br />
                批准后会顶替那条旧记录
              </template>
              <el-tag type="warning" size="small" class="ml">已知设备</el-tag>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column label="设备标识" width="140">
          <template #default="{ row }">
            <el-tooltip :content="row.device_key || row.device_uid || '—'" placement="top">
              <span class="mono">{{ shortKey(row.device_key || row.device_uid) }}</span>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column prop="os_version" label="系统" width="110" />
        <el-table-column prop="app_version" label="App" width="80" />
        <el-table-column prop="ip" label="IP" width="140" />
        <el-table-column label="配对码" width="110">
          <template #default="{ row }">
            <span class="pair">{{ row.pair_code }}</span>
          </template>
        </el-table-column>
        <el-table-column label="申请时间" width="170">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="160">
          <template #default="{ row }">
            <el-button link type="primary" @click="openApprove(row)">批准</el-button>
            <el-button link type="danger" @click="reject(row)">拒绝</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-card shadow="never" class="block">
      <template #header>
        <div class="head">
          <span>全部设备</span>
          <el-checkbox v-model="showExpired" size="small" @change="loadAll">显示已过期</el-checkbox>
        </div>
      </template>
      <el-table :data="all" v-loading="loading" stripe>
        <el-table-column prop="model" label="设备" min-width="150" />
        <el-table-column label="设备标识" width="140">
          <template #default="{ row }">
            <el-tooltip :content="row.device_key || row.device_uid || '—'" placement="top">
              <span class="mono">{{ shortKey(row.device_key || row.device_uid) }}</span>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column prop="os_version" label="系统" width="105" />
        <el-table-column prop="ip" label="IP" width="125" />
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="statusType(row.status)" size="small">{{ statusText(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="申请时间" width="165">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="最近活跃" width="165">
          <template #default="{ row }">
            {{ row.last_seen_at ? formatTime(row.last_seen_at) : '—' }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="110">
          <template #default="{ row }">
            <el-button
              v-if="row.status === 'approved'"
              link
              type="danger"
              @click="revoke(row)"
            >
              吊销
            </el-button>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="dialog" title="批准这台设备" width="520px">
      <el-alert
        v-if="autoFilled"
        type="success"
        :closable="false"
        show-icon
        class="mb"
        title="已自动填入上次的绑定"
        :description="`这台手机上次授权给「${current?.last_nickname || current?.last_account}」，确认无误直接点批准。`"
      />
      <el-form label-width="90px">
        <el-form-item label="设备">
          <span>{{ current?.model }} · {{ current?.os_version }} · {{ current?.ip }}</span>
        </el-form-item>
        <el-form-item label="配对码">
          <span class="pair">{{ current?.pair_code }}</span>
        </el-form-item>

        <el-form-item label="分配方式">
          <el-radio-group v-model="mode">
            <el-radio value="existing">绑定已有用户</el-radio>
            <el-radio value="new">新建用户</el-radio>
          </el-radio-group>
        </el-form-item>

        <el-form-item v-if="mode === 'existing'" label="用户">
          <el-select v-model="form.user_id" placeholder="选择用户" filterable style="width: 100%">
            <el-option
              v-for="u in users"
              :key="u.id"
              :label="`${u.nickname}（${u.account}）`"
              :value="u.id"
            />
          </el-select>
        </el-form-item>

        <template v-else>
          <el-form-item label="账号">
            <el-input v-model="form.new_account" placeholder="字母数字，3-32 位" />
          </el-form-item>
          <el-form-item label="昵称">
            <el-input v-model="form.new_nickname" placeholder="家人看到的名字" />
          </el-form-item>
          <el-form-item label="密码">
            <el-input v-model="form.new_password" placeholder="留空自动生成" />
          </el-form-item>
        </template>

        <el-form-item label="加入家庭">
          <el-select v-model="form.family_id" placeholder="可不选" clearable style="width: 100%">
            <el-option v-for="f in families" :key="f.id" :label="f.name" :value="f.id" />
          </el-select>
        </el-form-item>

        <el-form-item label="角色">
          <el-select v-model="form.role" style="width: 100%">
            <el-option label="成员" value="member" />
            <el-option label="管理员" value="admin" />
            <el-option label="户主" value="owner" />
          </el-select>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="dialog = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">批准并授权</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiGet, apiPost, formatTime } from '../api/client'

const pending = ref<any[]>([])
const all = ref<any[]>([])
const users = ref<any[]>([])
const families = ref<any[]>([])
const loading = ref(false)
const submitting = ref(false)
const dialog = ref(false)
const current = ref<any>(null)
const mode = ref<'existing' | 'new'>('existing')
const showExpired = ref(false)
const autoFilled = ref(false)

const form = ref({
  user_id: '',
  new_account: '',
  new_nickname: '',
  new_password: '',
  family_id: '',
  role: 'member',
})

async function loadAll() {
  loading.value = true
  try {
    const [p, a] = await Promise.all([
      apiGet('/devices/pending') as Promise<any>,
      apiGet('/devices', showExpired.value ? { include_expired: 1 } : {}) as Promise<any>,
    ])
    pending.value = p.items || []
    all.value = a.items || []
  } catch (e: any) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

async function loadOptions() {
  try {
    const [u, f] = await Promise.all([
      apiGet('/users', { page: 1, page_size: 200 }) as Promise<any>,
      apiGet('/families', { page: 1, page_size: 200 }) as Promise<any>,
    ])
    users.value = u.items || []
    families.value = f.items || []
  } catch (e: any) {
    // 选项加载失败不阻塞主流程
  }
}

function openApprove(row: any) {
  current.value = row
  mode.value = 'existing'
  // 同一台手机之前授权过：把上次的用户/家庭/角色自动填好，确认无误直接批准
  const known = !!row.last_user_id
  autoFilled.value = known
  form.value = {
    user_id: known ? row.last_user_id : '',
    new_account: '',
    new_nickname: '',
    new_password: '',
    family_id: known ? row.last_family_id || '' : '',
    role: known ? row.last_role || 'member' : 'member',
  }
  dialog.value = true
}

async function submit() {
  if (mode.value === 'existing' && !form.value.user_id) {
    ElMessage.warning('请选择要绑定的用户')
    return
  }
  if (mode.value === 'new' && (!form.value.new_account || !form.value.new_nickname)) {
    ElMessage.warning('请填写新用户的账号和昵称')
    return
  }
  // 同一台手机之前绑过别人：提醒一次，避免把一台设备挂到多个账号
  const prevUserId = current.value?.last_user_id
  if (prevUserId && mode.value === 'existing' && form.value.user_id && form.value.user_id !== prevUserId) {
    try {
      await ElMessageBox.confirm(
        `这台手机之前授权给「${current.value.last_nickname || current.value.last_account}」。\n` +
          '继续会把那条旧授权顶掉，你确定要改绑到别的账号吗？',
        '设备已授权给其他人',
        { type: 'warning', confirmButtonText: '仍然改绑', cancelButtonText: '取消' },
      )
    } catch {
      return
    }
  }
  submitting.value = true
  try {
    const payload: any = {
      family_id: form.value.family_id,
      role: form.value.role,
    }
    if (mode.value === 'existing') {
      payload.user_id = form.value.user_id
    } else {
      payload.new_account = form.value.new_account
      payload.new_nickname = form.value.new_nickname
      payload.new_password = form.value.new_password
    }
    const res: any = await apiPost(`/devices/${current.value.id}/approve`, payload)
    dialog.value = false
    if (res.generated_password) {
      ElMessageBox.alert(
        `已创建用户 ${res.account}，初始密码：${res.generated_password}\n请转告本人（登录后可改）。`,
        '用户已创建',
      )
    }
    ElMessage.success(
      res.superseded?.length
        ? `已批准，顶替了这台手机之前的 ${res.superseded.length} 条旧授权`
        : '已批准，设备几分钟内会自动登录',
    )
    await loadAll()
  } catch (e: any) {
    ElMessage.error(e.message)
  } finally {
    submitting.value = false
  }
}

async function reject(row: any) {
  try {
    await ElMessageBox.confirm(`拒绝「${row.model || row.device_uid}」的配对请求？`, '确认', {
      type: 'warning',
    })
    await apiPost(`/devices/${row.id}/reject`, {})
    ElMessage.success('已拒绝')
    await loadAll()
  } catch (e: any) {
    if (e !== 'cancel') ElMessage.error(e.message || '操作失败')
  }
}

async function revoke(row: any) {
  try {
    await ElMessageBox.confirm(
      `吊销「${row.model || row.device_uid}」？该设备会立即失去登录状态。`,
      '确认吊销',
      { type: 'warning' },
    )
    await apiPost(`/devices/${row.id}/revoke`, {})
    ElMessage.success('已吊销')
    await loadAll()
  } catch (e: any) {
    if (e !== 'cancel') ElMessage.error(e.message || '操作失败')
  }
}

function statusText(s: string) {
  return (
    {
      pending: '待授权',
      approved: '已授权',
      rejected: '已拒绝',
      expired: '已过期',
      revoked: '已吊销',
      superseded: '已顶替',
    } as any
  )[s] || s
}

function statusType(s: string) {
  return (
    {
      pending: 'warning',
      approved: 'success',
      rejected: 'info',
      expired: 'info',
      revoked: 'danger',
      superseded: 'info',
    } as any
  )[s] || 'info'
}

/** 设备标识只显示头部，完整值在 tooltip 里 */
function shortKey(k?: string) {
  if (!k) return '—'
  if (k.length <= 14) return k
  return k.slice(0, 10) + '…' + k.slice(-4)
}

onMounted(() => {
  loadAll()
  loadOptions()
})
</script>

<style scoped>
.mb {
  margin-bottom: 14px;
}
.block {
  margin-bottom: 16px;
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.mr {
  margin-right: 8px;
}
.muted {
  color: #b0b7c3;
}
.mono {
  font-family: ui-monospace, Menlo, monospace;
  font-size: 12px;
  color: #5b6472;
}
.ml {
  margin-left: 6px;
}
.pair {
  font-family: ui-monospace, Menlo, monospace;
  font-size: 15px;
  letter-spacing: 2px;
  font-weight: 600;
  color: #2f6f62;
}
</style>

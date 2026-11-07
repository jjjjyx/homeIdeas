<template>
  <el-card shadow="never">
    <div class="toolbar">
      <el-input
        v-model="keyword"
        placeholder="搜索账号或昵称"
        clearable
        style="width: 240px"
        @keyup.enter="reload"
        @clear="reload"
      />
      <el-button type="primary" @click="reload">查询</el-button>
      <el-button type="success" @click="openAdd">添加成员</el-button>
      <el-button @click="openInvite">邀请码</el-button>
      <span class="total">共 {{ total }} 位成员</span>
    </div>

    <!-- 待授权设备：新设备首次加入时批准一次，之后永久记住 -->
    <div v-if="pending.length" class="pending">
      <div class="pending-head">待授权设备（{{ pending.length }}）</div>
      <div v-for="d in pending" :key="d.id" class="pending-row">
        <span class="dev">{{ d.model || '未知机型' }} · {{ d.os_version }}</span>
        <span class="muted">{{ shortKey(d.device_key) }}</span>
        <span v-if="d.last_account" class="muted">
          上次是「{{ d.last_nickname || d.last_account }}」
        </span>
        <el-select
          v-model="approveTarget[d.id]"
          placeholder="选择成员"
          size="small"
          style="width: 150px"
        >
          <el-option v-for="m in items" :key="m.id" :label="m.nickname" :value="m.id" />
        </el-select>
        <el-button size="small" type="primary" @click="approve(d)">批准</el-button>
        <el-button size="small" @click="rejectDevice(d)">拒绝</el-button>
      </div>
    </div>

    <el-table :data="items" v-loading="loading" stripe @expand-change="onExpand">
      <el-table-column type="expand">
        <template #default="{ row }">
          <div class="devices">
            <div v-if="!devices[row.id]?.length" class="muted">还没有授权的设备</div>
            <div v-for="d in devices[row.id]" :key="d.id" class="device-row">
              <span class="dev">{{ d.model || '未知机型' }} · {{ d.os_version }}</span>
              <span class="muted">{{ shortKey(d.device_key) }}</span>
              <span class="muted">
                最后使用 {{ d.last_seen_at ? formatTime(d.last_seen_at) : '—' }}
              </span>
              <el-button link type="danger" size="small" @click="unbind(row, d)">解绑</el-button>
            </div>
          </div>
        </template>
      </el-table-column>
      <el-table-column prop="nickname" label="昵称" width="150" />
      <el-table-column prop="account" label="账号" width="160" />
      <el-table-column label="角色" width="110">
        <template #default="{ row }">
          <el-tag :type="roleType(row.role)" size="small">{{ roleText(row.role) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="row.status === 1 ? 'success' : 'danger'" size="small">
            {{ row.status === 1 ? '正常' : '已禁用' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="加入时间" width="170">
        <template #default="{ row }">{{ formatTime(row.joined_at) }}</template>
      </el-table-column>
      <el-table-column label="操作">
        <template #default="{ row }">
          <el-button
            v-if="canChangeRole(row)"
            link
            type="primary"
            @click="openRole(row)"
          >
            改角色
          </el-button>
          <el-button link type="primary" @click="openPassword(row)" v-if="canReset(row)">重置密码</el-button>
          <el-button
            v-if="canDisable(row)"
            link
            :type="row.status === 1 ? 'danger' : 'success'"
            @click="toggle(row)"
          >
            {{ row.status === 1 ? '禁用' : '解禁' }}
          </el-button>
          <el-button v-if="canRemove(row)" link type="danger" @click="remove(row)">移除</el-button>
          <span v-if="row.id === myId" class="muted">（我）</span>
        </template>
      </el-table-column>
    </el-table>

    <el-pagination
      class="pager"
      layout="prev, pager, next"
      :page-size="pageSize"
      :total="total"
      :current-page="page"
      @current-change="onPage"
    />

    <el-dialog v-model="addDialog" title="添加家庭成员" width="460px">
      <el-form label-width="80px">
        <el-form-item label="账号">
          <el-input v-model="addForm.account" placeholder="3-32 位小写字母/数字/._-" />
        </el-form-item>
        <el-form-item label="昵称">
          <el-input v-model="addForm.nickname" placeholder="家人看到的名字" />
        </el-form-item>
        <el-form-item label="初始密码">
          <el-input v-model="addForm.password" placeholder="至少 8 位" show-password />
        </el-form-item>
        <el-form-item label="角色">
          <el-select v-model="addForm.role" style="width: 100%">
            <el-option label="成员" value="member" />
            <el-option v-if="myRole === 'owner'" label="管理员" value="admin" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="addDialog = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitAdd">创建</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="roleDialog" title="调整角色" width="380px">
      <el-radio-group v-model="roleForm.role">
        <el-radio value="member">成员</el-radio>
        <el-radio value="admin">管理员</el-radio>
      </el-radio-group>
      <template #footer>
        <el-button @click="roleDialog = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitRole">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="passwordDialog" title="重置密码" width="420px">
      <p class="hint">重置后该成员的所有设备登录会失效，需要用新密码重新授权设备。</p>
      <el-input v-model="passwordForm.password" placeholder="至少 8 位" show-password />
      <template #footer>
        <el-button @click="passwordDialog = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitPassword">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="inviteDialog" title="生成邀请码" width="440px">
      <p class="hint">
        邀请码用于新成员在 App 启动页「用邀请码注册」；72 小时内有效，仅可使用一次。
      </p>
      <el-form label-width="70px">
        <el-form-item label="角色">
          <el-select v-model="inviteRole" style="width: 100%">
            <el-option label="成员" value="member" />
            <el-option v-if="myRole === 'owner'" label="管理员" value="admin" />
          </el-select>
        </el-form-item>
      </el-form>
      <div v-if="inviteCode" class="invite-box">
        <span class="invite-code">{{ inviteCode }}</span>
      </div>
      <template #footer>
        <el-button @click="inviteDialog = false">关闭</el-button>
        <el-button v-if="inviteCode" @click="copyInvite">复制</el-button>
        <el-button type="primary" :loading="inviting" @click="createInvite">
          {{ inviteCode ? '再生成一个' : '生成邀请码' }}
        </el-button>
      </template>
    </el-dialog>
  </el-card>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiDelete, apiGet, apiPatch, apiPost, formatTime } from '../api/client'
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const items = ref<any[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const keyword = ref('')
const loading = ref(false)
const submitting = ref(false)

const addDialog = ref(false)
const roleDialog = ref(false)
const passwordDialog = ref(false)
const inviteDialog = ref(false)
const current = ref<any>(null)
const inviteRole = ref('member')
const inviteCode = ref('')
const inviting = ref(false)

const addForm = ref({ account: '', nickname: '', password: '', role: 'member' })
const roleForm = ref({ role: 'member' })
const passwordForm = ref({ password: '' })

const myId = computed(() => auth.user?.id || '')
const myRole = computed(() => auth.user?.role || '')

function roleText(role: string) {
  return role === 'owner' ? '户主' : role === 'admin' ? '管理员' : '成员'
}
function roleType(role: string) {
  return role === 'owner' ? 'warning' : role === 'admin' ? 'success' : 'info'
}
function canChangeRole(row: any) {
  return myRole.value === 'owner' && row.role !== 'owner' && row.id !== myId.value
}
function canDisable(row: any) {
  if (row.role === 'owner' || row.id === myId.value) return false
  return myRole.value === 'owner' || row.role === 'member'
}
function canRemove(row: any) {
  if (row.role === 'owner' || row.id === myId.value) return false
  return myRole.value === 'owner' || row.role === 'member'
}
function canReset(row: any) {
  if (row.id === myId.value || row.role === 'owner') return false
  return myRole.value === 'owner' || row.role === 'member'
}

// ---------- 设备 ----------
const pending = ref<any[]>([])
const devices = ref<Record<string, any[]>>({})
const approveTarget = ref<Record<string, string>>({})

async function loadPending() {
  try {
    const data: any = await apiGet('/devices/pending')
    pending.value = data.items || []
  } catch (e: any) {
    // 静默失败，不影响成员列表
  }
}

async function onExpand(row: any, expanded: any[]) {
  const open = Array.isArray(expanded) ? expanded.some((r: any) => r.id === row.id) : !!expanded
  if (!open) return
  try {
    const data: any = await apiGet(`/members/${row.id}/devices`)
    devices.value = { ...devices.value, [row.id]: data.items || [] }
  } catch (e: any) {
    ElMessage.error(e.message)
  }
}

async function approve(d: any) {
  const target = approveTarget.value[d.id]
  if (!target) {
    ElMessage.warning('先选一个成员，再点批准')
    return
  }
  try {
    const r: any = await apiPost(`/devices/${d.id}/approve`, { user_id: target })
    ElMessage.success(
      r?.superseded?.length ? `已批准，顶替了 ${r.superseded.length} 条旧记录` : '已批准，这台设备以后永久免批',
    )
    await loadPending()
    await load()
  } catch (e: any) {
    ElMessage.error(e.message)
  }
}

async function rejectDevice(d: any) {
  try {
    await ElMessageBox.confirm('拒绝这台设备的授权申请？', '确认', { type: 'warning' })
    await apiPost(`/devices/${d.id}/reject`, {})
    ElMessage.success('已拒绝')
    await loadPending()
  } catch (e: any) {
    if (e !== 'cancel') ElMessage.error(e.message || '操作失败')
  }
}

async function unbind(member: any, d: any) {
  try {
    await ElMessageBox.confirm(
      `解绑「${member.nickname}」的这台设备？解绑后这台手机需要重新批准才能登录。`,
      '确认解绑',
      { type: 'warning' },
    )
    await apiDelete(`/members/${member.id}/devices/${d.id}`)
    ElMessage.success('已解绑')
    devices.value = { ...devices.value, [member.id]: [] }
    await onExpand(member, [member])
  } catch (e: any) {
    if (e !== 'cancel') ElMessage.error(e.message || '操作失败')
  }
}

function shortKey(k?: string) {
  if (!k) return '—'
  return k.length <= 14 ? k : k.slice(0, 10) + '…' + k.slice(-4)
}
async function load() {
  loading.value = true
  try {
    const data: any = await apiGet('/members', { q: keyword.value, page: page.value, page_size: pageSize })
    items.value = data.items
    total.value = data.total
  } catch (e: any) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

function reload() {
  page.value = 1
  load()
}

function onPage(p: number) {
  page.value = p
  load()
}

function openAdd() {
  addForm.value = { account: '', nickname: '', password: '', role: 'member' }
  addDialog.value = true
}

async function submitAdd() {
  const f = addForm.value
  if (!f.account || !f.nickname || !f.password) {
    ElMessage.warning('请填写账号、昵称和初始密码')
    return
  }
  submitting.value = true
  try {
    await apiPost('/members', { ...f, account: f.account.trim(), nickname: f.nickname.trim() })
    ElMessage.success('成员已创建')
    addDialog.value = false
    reload()
  } catch (e: any) {
    ElMessage.error(e.message)
  } finally {
    submitting.value = false
  }
}

function openRole(row: any) {
  current.value = row
  roleForm.value = { role: row.role }
  roleDialog.value = true
}

async function submitRole() {
  submitting.value = true
  try {
    await apiPatch(`/members/${current.value.id}`, { role: roleForm.value.role })
    ElMessage.success('角色已更新')
    roleDialog.value = false
    load()
  } catch (e: any) {
    ElMessage.error(e.message)
  } finally {
    submitting.value = false
  }
}

function openPassword(row: any) {
  current.value = row
  passwordForm.value = { password: '' }
  passwordDialog.value = true
}

async function submitPassword() {
  if (!passwordForm.value.password) {
    ElMessage.warning('请输入新密码')
    return
  }
  submitting.value = true
  try {
    await apiPatch(`/members/${current.value.id}`, { password: passwordForm.value.password })
    ElMessage.success('密码已重置')
    passwordDialog.value = false
  } catch (e: any) {
    ElMessage.error(e.message)
  } finally {
    submitting.value = false
  }
}

async function toggle(row: any) {
  const status = row.status === 1 ? 0 : 1
  const action = status === 1 ? '解禁' : '禁用'
  try {
    await ElMessageBox.confirm(`确定要${action}成员「${row.nickname}」吗？`, '确认', { type: 'warning' })
  } catch {
    return
  }
  try {
    await apiPatch(`/members/${row.id}`, { status })
    ElMessage.success(`已${action}`)
    load()
  } catch (e: any) {
    ElMessage.error(e.message)
  }
}

async function remove(row: any) {  try {
    await ElMessageBox.confirm(
      `确定要把「${row.nickname}」移出家庭吗？其设备授权会立即失效。`,
      '移除成员',
      { type: 'warning' },
    )
  } catch {
    return
  }
  try {
    await apiDelete(`/members/${row.id}`)
    ElMessage.success('已移除')
    load()
  } catch (e: any) {
    ElMessage.error(e.message)
  }
}

function openInvite() {
  inviteRole.value = 'member'
  inviteCode.value = ''
  inviteDialog.value = true
}

async function createInvite() {
  inviting.value = true
  try {
    const data: any = await apiPost('/invites', { role: inviteRole.value, expires_in_hours: 72 })
    inviteCode.value = data.code
    ElMessage.success('邀请码已生成')
  } catch (e: any) {
    ElMessage.error(e.message)
  } finally {
    inviting.value = false
  }
}

async function copyInvite() {
  try {
    await navigator.clipboard.writeText(inviteCode.value)
    ElMessage.success('已复制')
  } catch {
    ElMessage.warning('浏览器不允许自动复制，请手动选中复制')
  }
}

onMounted(() => {
  load()
  loadPending()
})
</script>

<style scoped>
.pending {
  margin-bottom: 16px;
  padding: 12px 14px;
  background: #fff8e8;
  border: 1px solid #f2d9a0;
  border-radius: 10px;
}
.pending-head {
  font-weight: 600;
  margin-bottom: 8px;
  color: #a8761a;
}
.pending-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 6px 0;
  flex-wrap: wrap;
}
.devices {
  padding: 8px 16px 12px 48px;
}
.device-row {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 6px 0;
  flex-wrap: wrap;
}
.dev {
  font-weight: 500;
}
.toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 14px;
}
.total {
  margin-left: auto;
  font-size: 13px;
  color: #909399;
}
.pager {
  margin-top: 16px;
  justify-content: flex-end;
}
.muted {
  color: #a8abb2;
  font-size: 12px;
  margin-left: 8px;
}
.hint {
  font-size: 12px;
  color: #909399;
  margin: 0 0 12px;
}
.invite-box {
  display: flex;
  justify-content: center;
  padding: 16px;
  background: #f4f8f7;
  border-radius: 10px;
}
.invite-code {
  font-family: ui-monospace, Menlo, monospace;
  font-size: 30px;
  font-weight: 700;
  letter-spacing: 8px;
  color: #2f6f62;
}
</style>

<template>
  <div>
    <el-card shadow="never" class="family">
      <div class="family-row">
        <div>
          <div class="label">当前家庭</div>
          <div class="name">{{ family?.name || '尚未初始化' }}</div>
          <div class="meta" v-if="family">
            创建于 {{ formatTime(family.created_at) }} · 同步版本 rev {{ family.rev }}
          </div>
          <div class="meta" v-else>请先在 App 或接口完成首次初始化</div>
        </div>
        <el-button v-if="family" type="primary" plain @click="openRename">修改家庭名称</el-button>
      </div>
    </el-card>

    <el-row :gutter="16" class="stats">
      <el-col :span="6" v-for="card in cards" :key="card.label">
        <el-card shadow="never" class="stat">
          <div class="label">{{ card.label }}</div>
          <div class="value">{{ card.value }}</div>
          <div class="extra">{{ card.extra }}</div>
        </el-card>
      </el-col>
    </el-row>

    <el-card shadow="never" class="tip">
      <p>系统只围绕这一个家庭运行：没有家庭列表、创建/加入家庭或切换家庭的入口。</p>
      <ul>
        <li>家庭成员由户主/管理员在「家庭成员」页直接创建，设备在「设备授权」页批准后自动登录</li>
        <li>后台只做查看、成员与设备治理，不代替家人记账；账目以 App 端同步为准</li>
        <li>账号被禁用或被移除后，其设备令牌会立即失效</li>
      </ul>
    </el-card>

    <el-dialog v-model="dialog" title="修改家庭名称" width="420px">
      <el-input v-model="newName" maxlength="20" show-word-limit placeholder="例如：我们的家" />
      <template #footer>
        <el-button @click="dialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveName">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { apiGet, apiPatch, formatMoney, formatTime } from '../api/client'

const overview = ref<any>({})
const dialog = ref(false)
const saving = ref(false)
const newName = ref('')

const family = computed(() => overview.value.family)

const cards = computed(() => [
  { label: '家庭成员', value: overview.value.members ?? 0, extra: `今日新增 ${overview.value.new_users_today ?? 0}` },
  { label: '账单总数', value: overview.value.transactions ?? 0, extra: `今日新增 ${overview.value.new_tx_today ?? 0}` },
  { label: '累计支出', value: `¥${formatMoney(overview.value.expense_total_cents)}`, extra: '不含资金往来与已删除' },
  { label: '家庭角色', value: roleLabel(family.value?.my_role), extra: '当前登录账号' },
])

function roleLabel(role?: string) {
  return role === 'owner' ? '户主' : role === 'admin' ? '管理员' : '—'
}

async function load() {
  try {
    overview.value = await apiGet('/overview')
  } catch (e: any) {
    ElMessage.error(e.message)
  }
}

function openRename() {
  newName.value = family.value?.name || ''
  dialog.value = true
}

async function saveName() {
  const name = newName.value.trim()
  if (!name) {
    ElMessage.warning('家庭名称不能为空')
    return
  }
  saving.value = true
  try {
    await apiPatch('/family', { name })
    ElMessage.success('已修改')
    dialog.value = false
    await load()
  } catch (e: any) {
    ElMessage.error(e.message)
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.family {
  border-radius: 12px;
  margin-bottom: 16px;
}
.family-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.name {
  font-size: 24px;
  font-weight: 700;
  margin: 6px 0 4px;
}
.label {
  font-size: 13px;
  color: #909399;
}
.meta {
  font-size: 12px;
  color: #a8abb2;
}
.stats {
  margin-bottom: 16px;
}
.stat {
  border-radius: 12px;
}
.value {
  font-size: 26px;
  font-weight: 700;
  margin: 8px 0 4px;
}
.extra {
  font-size: 12px;
  color: #a8abb2;
}
.tip {
  border-radius: 12px;
  color: #606266;
  font-size: 13px;
}
</style>

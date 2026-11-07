<template>
  <div>
    <el-row :gutter="16">
      <el-col :span="6" v-for="card in cards" :key="card.label">
        <el-card shadow="never" class="stat">
          <div class="label">{{ card.label }}</div>
          <div class="value">{{ card.value }}</div>
          <div class="extra">{{ card.extra }}</div>
        </el-card>
      </el-col>
    </el-row>

    <el-card shadow="never" class="tip">
      <p>这里只做平台治理，不代替用户记账。</p>
      <ul>
        <li>用户被禁用后，其所有 refresh token 会立即失效</li>
        <li>账目数据由家庭成员各自维护，后台仅查看与审计</li>
      </ul>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { apiGet, formatMoney } from '../api/client'

const overview = ref<any>({})

const cards = computed(() => [
  { label: '用户总数', value: overview.value.users ?? 0, extra: `今日新增 ${overview.value.new_users_today ?? 0}` },
  { label: '家庭总数', value: overview.value.families ?? 0, extra: '当前有效家庭' },
  { label: '账单总数', value: overview.value.transactions ?? 0, extra: `今日新增 ${overview.value.new_tx_today ?? 0}` },
  { label: '累计支出', value: `¥${formatMoney(overview.value.expense_total_cents)}`, extra: '不含已删除' },
])

onMounted(async () => {
  try {
    overview.value = await apiGet('/overview')
  } catch (e: any) {
    ElMessage.error(e.message)
  }
})
</script>

<style scoped>
.stat {
  border-radius: 12px;
}
.label {
  font-size: 13px;
  color: #909399;
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
  margin-top: 18px;
  border-radius: 12px;
  color: #606266;
  font-size: 13px;
}
</style>

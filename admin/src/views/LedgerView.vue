<template>
  <el-card shadow="never">
    <div class="toolbar">
      <span class="title">家庭全部账目</span>
      <span class="total">共 {{ total }} 条（含已删除，便于审计）</span>
    </div>

    <el-table :data="items" v-loading="loading" stripe size="small">
      <el-table-column label="日期" width="120">
        <template #default="{ row }">{{ formatTime(row.occurred_at) }}</template>
      </el-table-column>
      <el-table-column prop="category_name" label="分类" width="110" />
      <el-table-column prop="member_name" label="记账人" width="110" />
      <el-table-column label="类型" width="80">
        <template #default="{ row }">
          <el-tag :type="row.type === 1 ? 'success' : 'danger'" size="small">
            {{ row.type === 1 ? '收入' : '支出' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="金额" width="120" align="right">
        <template #default="{ row }">¥{{ formatMoney(row.amount_cents) }}</template>
      </el-table-column>
      <el-table-column prop="note" label="备注" show-overflow-tooltip min-width="180" />
      <el-table-column label="可见性" width="90">
        <template #default="{ row }">
          <el-tag size="small" :type="row.visibility === 'private' ? 'info' : 'primary'">
            {{ row.visibility === 'private' ? '私密' : '家庭' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="80">
        <template #default="{ row }">
          <span v-if="row.deleted_at" class="deleted">已删除</span>
          <span v-else class="muted">正常</span>
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
  </el-card>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { apiGet, formatMoney, formatTime } from '../api/client'

const items = ref<any[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 50
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const data: any = await apiGet('/transactions', { page: page.value, page_size: pageSize })
    items.value = data.items
    total.value = data.total
  } catch (e: any) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

function onPage(p: number) {
  page.value = p
  load()
}

onMounted(load)
</script>

<style scoped>
.toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 14px;
}
.title {
  font-size: 15px;
  font-weight: 600;
}
.total {
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
}
.deleted {
  color: #f56c6c;
  font-size: 12px;
}
</style>

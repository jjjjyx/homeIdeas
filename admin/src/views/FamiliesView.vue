<template>
  <el-card shadow="never">
    <div class="toolbar">
      <el-input
        v-model="keyword"
        placeholder="搜索家庭名称"
        clearable
        style="width: 240px"
        @keyup.enter="reload"
        @clear="reload"
      />
      <el-button type="primary" @click="reload">查询</el-button>
    </div>

    <el-table :data="items" v-loading="loading" stripe>
      <el-table-column prop="name" label="家庭名称" width="180" />
      <el-table-column prop="owner_nickname" label="户主" width="140" />
      <el-table-column label="成员数" width="90">
        <template #default="{ row }">{{ row.member_count }}</template>
      </el-table-column>
      <el-table-column label="账单数" width="90">
        <template #default="{ row }">{{ row.transaction_count }}</template>
      </el-table-column>
      <el-table-column label="同步版本" width="100">
        <template #default="{ row }">{{ row.rev }}</template>
      </el-table-column>
      <el-table-column label="创建时间" width="170">
        <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="操作">
        <template #default="{ row }">
          <el-button link type="primary" @click="openTx(row)">查看账目</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-pagination
      class="pager"
      layout="prev, pager, next"
      :page-size="20"
      :total="total"
      :current-page="page"
      @current-change="onPage"
    />
  </el-card>

  <el-dialog v-model="dialogVisible" :title="`账目 · ${current?.name || ''}`" width="820px">
    <el-table :data="txs" v-loading="txLoading" max-height="420" size="small">
      <el-table-column label="日期" width="120">
        <template #default="{ row }">{{ formatTime(row.occurred_at) }}</template>
      </el-table-column>
      <el-table-column prop="category_name" label="分类" width="100" />
      <el-table-column prop="member_name" label="记账人" width="100" />
      <el-table-column label="类型" width="80">
        <template #default="{ row }">
          <el-tag :type="row.type === 1 ? 'success' : 'danger'" size="small">
            {{ row.type === 1 ? '收入' : '支出' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="金额" width="120">
        <template #default="{ row }">¥{{ formatMoney(row.amount_cents) }}</template>
      </el-table-column>
      <el-table-column prop="note" label="备注" show-overflow-tooltip />
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
  </el-dialog>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { apiGet, formatMoney, formatTime } from '../api/client'

const items = ref<any[]>([])
const keyword = ref('')
const page = ref(1)
const total = ref(0)
const loading = ref(false)

const dialogVisible = ref(false)
const current = ref<any>(null)
const txs = ref<any[]>([])
const txLoading = ref(false)

async function load() {
  loading.value = true
  try {
    const data: any = await apiGet('/families', { q: keyword.value, page: page.value, page_size: 20 })
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

async function openTx(row: any) {
  current.value = row
  dialogVisible.value = true
  txLoading.value = true
  try {
    const data: any = await apiGet(`/families/${row.id}/transactions`, { page: 1, page_size: 100 })
    txs.value = data.items
  } catch (e: any) {
    ElMessage.error(e.message)
  } finally {
    txLoading.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 14px;
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

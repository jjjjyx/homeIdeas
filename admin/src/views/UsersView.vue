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
      <span class="total">共 {{ total }} 个用户</span>
    </div>

    <el-table :data="items" v-loading="loading" stripe>
      <el-table-column prop="account" label="账号" width="160" />
      <el-table-column prop="nickname" label="昵称" width="150" />
      <el-table-column label="家庭数" width="90">
        <template #default="{ row }">{{ row.family_count }}</template>
      </el-table-column>
      <el-table-column label="角色" width="110">
        <template #default="{ row }">
          <el-tag v-if="row.is_platform_admin" type="warning" size="small">平台管理员</el-tag>
          <span v-else class="muted">普通用户</span>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="row.status === 1 ? 'success' : 'danger'" size="small">
            {{ row.status === 1 ? '正常' : '已禁用' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="注册时间" width="170">
        <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="操作">
        <template #default="{ row }">
          <span v-if="row.is_platform_admin" class="muted">—</span>
          <el-button v-else-if="row.status === 1" link type="danger" @click="toggle(row, 0)">
            禁用
          </el-button>
          <el-button v-else link type="success" @click="toggle(row, 1)">解禁</el-button>
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
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiGet, apiPatch, formatTime } from '../api/client'

const items = ref<any[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const keyword = ref('')
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const data: any = await apiGet('/users', { q: keyword.value, page: page.value, page_size: pageSize })
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

async function toggle(row: any, status: number) {
  const action = status === 1 ? '解禁' : '禁用'
  try {
    await ElMessageBox.confirm(`确定要${action}用户「${row.nickname}」吗？`, '确认', {
      type: 'warning',
    })
  } catch {
    return
  }
  try {
    await apiPatch(`/users/${row.id}`, { status })
    ElMessage.success(`已${action}`)
    load()
  } catch (e: any) {
    ElMessage.error(e.message)
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
  font-size: 13px;
}
</style>

<template>
  <el-card shadow="never">
    <div class="toolbar">
      <el-select v-model="action" placeholder="操作类型" clearable style="width: 220px" @change="reload">
        <el-option label="全部" value="" />
        <el-option label="注册" value="auth.register" />
        <el-option label="登录" value="auth.login" />
        <el-option label="管理后台登录" value="admin.login" />
        <el-option label="建房" value="family.create" />
        <el-option label="邀请" value="family.invite" />
        <el-option label="加入" value="family.join" />
        <el-option label="移除成员" value="member.remove" />
        <el-option label="删账" value="transaction.delete" />
        <el-option label="封禁用户" value="admin.user.disable" />
      </el-select>
    </div>

    <el-table :data="items" v-loading="loading" stripe size="small">
      <el-table-column label="时间" width="170">
        <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column prop="action" label="操作" width="180" />
      <el-table-column prop="nickname" label="操作人" width="120" />
      <el-table-column prop="target_type" label="对象类型" width="110" />
      <el-table-column prop="target_id" label="对象 ID" show-overflow-tooltip />
      <el-table-column prop="detail" label="详情" show-overflow-tooltip width="160" />
      <el-table-column prop="ip" label="IP" width="130" />
    </el-table>

    <el-pagination
      class="pager"
      layout="prev, pager, next"
      :page-size="30"
      :total="total"
      :current-page="page"
      @current-change="onPage"
    />
  </el-card>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { apiGet, formatTime } from '../api/client'

const items = ref<any[]>([])
const action = ref('')
const page = ref(1)
const total = ref(0)
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const data: any = await apiGet('/audit-logs', { action: action.value, page: page.value, page_size: 30 })
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
</style>

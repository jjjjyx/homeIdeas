<template>
  <div class="login-page">
    <el-card class="card" shadow="always">
      <h2 class="title">家庭记账本</h2>
      <p class="sub">平台管理后台</p>
      <el-form @submit.prevent="onSubmit">
        <el-form-item>
          <el-input v-model="account" placeholder="管理员账号" size="large" clearable />
        </el-form-item>
        <el-form-item>
          <el-input
            v-model="password"
            type="password"
            show-password
            placeholder="密码"
            size="large"
            @keyup.enter="onSubmit"
          />
        </el-form-item>
        <el-button type="primary" size="large" :loading="loading" style="width: 100%" @click="onSubmit">
          登 录
        </el-button>
      </el-form>
      <p class="hint">仅平台管理员可登录，App 账号无法进入</p>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../stores/auth'

const account = ref('')
const password = ref('')
const loading = ref(false)
const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

async function onSubmit() {
  if (!account.value || !password.value) {
    ElMessage.warning('请输入账号和密码')
    return
  }
  loading.value = true
  try {
    await auth.login(account.value.trim(), password.value)
    ElMessage.success('登录成功')
    const redirect = (route.query.redirect as string) || '/dashboard'
    router.push(redirect)
  } catch (e: any) {
    ElMessage.error(e.message || '登录失败')
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-page {
  height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #1f3b36, #2f6f62);
}
.card {
  width: 360px;
  border-radius: 14px;
  padding: 8px 6px 4px;
}
.title {
  margin: 6px 0 2px;
  font-size: 20px;
  text-align: center;
}
.sub {
  margin: 0 0 22px;
  text-align: center;
  font-size: 12px;
  color: #909399;
}
.hint {
  margin-top: 14px;
  font-size: 12px;
  color: #a8abb2;
  text-align: center;
}
</style>

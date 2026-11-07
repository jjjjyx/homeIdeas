<template>
  <el-container class="layout">
    <el-aside width="208px" class="aside">
      <div class="brand">家庭记账本</div>
      <div class="brand-sub">单家庭管理后台</div>
      <el-menu :default-active="$route.path" router class="menu">
        <el-menu-item index="/dashboard">家庭概览</el-menu-item>
        <el-menu-item index="/ledger">账目</el-menu-item>
        <el-menu-item index="/members">家庭成员</el-menu-item>
        <el-menu-item index="/audit">审计日志</el-menu-item>
      </el-menu>
    </el-aside>

    <el-container>
      <el-header class="header">
        <span class="title">{{ $route.meta.title || '' }}</span>
        <div class="right">
          <span class="who">{{ auth.displayName }}</span>
          <el-button link type="primary" @click="onLogout">退出登录</el-button>
        </div>
      </el-header>
      <el-main>
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const router = useRouter()

function onLogout() {
  auth.logout()
  router.push('/login')
}
</script>

<style scoped>
.layout {
  height: 100vh;
}
.aside {
  background: #1f3b36;
  color: #fff;
  display: flex;
  flex-direction: column;
}
.brand {
  font-size: 17px;
  font-weight: 700;
  padding: 22px 20px 2px;
  color: #fff;
}
.brand-sub {
  font-size: 12px;
  padding: 0 20px 14px;
  color: rgba(255, 255, 255, 0.55);
}
.menu {
  border-right: none;
  background: transparent;
  --el-menu-bg-color: transparent;
  --el-menu-text-color: rgba(255, 255, 255, 0.75);
  --el-menu-active-color: #fff;
  --el-menu-hover-bg-color: rgba(255, 255, 255, 0.08);
}
.menu :deep(.el-menu-item.is-active) {
  background: rgba(255, 255, 255, 0.14);
}
.header {
  background: #fff;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid #ebeef5;
}
.title {
  font-size: 16px;
  font-weight: 600;
}
.right {
  display: flex;
  align-items: center;
  gap: 12px;
}
.who {
  font-size: 13px;
  color: #606266;
}
</style>

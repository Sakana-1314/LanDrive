<script setup lang="ts">
// 404：地址不存在时的兜底页。
import { useRouter } from 'vue-router'
import { NButton, NIcon } from 'naive-ui'
import { CompassOutline } from '@vicons/ionicons5'

const router = useRouter()
</script>

<template>
  <div class="nf-page">
    <div class="nf-card">
      <div class="nf-icon" aria-hidden="true">
        <n-icon :size="34"><compass-outline /></n-icon>
      </div>
      <div class="nf-code">404</div>
      <p class="nf-text">这个地址不存在</p>
      <n-button type="primary" @click="router.push('/files')">返回文件列表</n-button>
    </div>
  </div>
</template>

<style scoped>
.nf-page {
  display: grid;
  min-height: 100dvh;
  padding: 24px;
  place-items: center;
  background: var(--page-glow), var(--color-bg);
}

.nf-card {
  display: grid;
  padding: 34px 40px;
  border: 1px solid var(--color-border-subtle);
  border-radius: 18px;
  background: var(--color-surface);
  box-shadow: var(--shadow-float);
  justify-items: center;
}

.nf-icon {
  display: grid;
  width: 60px;
  height: 60px;
  margin-bottom: 14px;
  place-items: center;
  border-radius: 18px;
  color: var(--color-primary);
  background: var(--color-primary-soft);
}

.nf-code {
  color: var(--color-text-strong);
  font-size: 30px;
  font-weight: 700;
  letter-spacing: 1px;
}

.nf-text {
  margin: 6px 0 20px;
  color: var(--color-text-muted);
  font-size: 14px;
}
</style>

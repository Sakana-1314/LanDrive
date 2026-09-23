<script setup lang="ts">
import { useRouter } from 'vue-router'
import { NButton, NResult } from 'naive-ui'

const router = useRouter()
</script>

<template>
  <div style="height: 100vh; display: flex; align-items: center; justify-content: center">
    <n-result status="404" title="页面不存在" description="请检查地址是否正确，或返回文件列表继续浏览。">
      <template #footer>
        <n-button type="primary" @click="router.push('/files')">返回文件列表</n-button>
      </template>
    </n-result>
  </div>
</template>

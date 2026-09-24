<script setup lang="ts">
// 整页拖放上传：拖文件到「我的文件」页面任意位置松手即上传。
//
// 为什么不再用虚线框：单独一块拖拽区会让人以为只有框里能拖，
// 而实际上整个列表、面包屑、空白处都可以是落点。所以改成
// 「页面级拖放 + 拖入时给出整页提示」，入口本身不占版面。
//
// 判断"拖的是文件"必须看 dataTransfer.types：拖动页面内元素（比如文件行）
// 也会触发 dragenter/dragover，照单全收会弹出毫无意义的提示。
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { NIcon } from 'naive-ui'
import { CloudUploadOutline } from '@vicons/ionicons5'

const props = withDefaults(
  defineProps<{
    /** 暂停上传时不再提示"松手即可上传"，否则用户拖进去才发现被拒。 */
    disabled?: boolean
  }>(),
  { disabled: false }
)

const emit = defineEmits<{ (e: 'files', files: File[]): void }>()

/** 嵌套元素间移动会连续触发 dragenter/dragleave，用计数抵消抖动。 */
let depth = 0
const active = ref(false)

/** 只有外部文件（含从系统文件夹拖来的文件夹）才算数，页面内元素拖动不算。 */
function carriesFiles(e: DragEvent): boolean {
  const t = e.dataTransfer?.types
  if (!t) return false
  return Array.prototype.includes.call(t, 'Files')
}

function onDragEnter(e: DragEvent) {
  if (props.disabled || !carriesFiles(e)) return
  e.preventDefault()
  depth++
  active.value = true
}

function onDragOver(e: DragEvent) {
  if (props.disabled || !carriesFiles(e)) return
  // 不 preventDefault 的话浏览器会拒绝这次放置，dragover 不给落点
  e.preventDefault()
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'copy'
}

function onDragLeave(e: DragEvent) {
  if (!active.value) return
  depth = Math.max(0, depth - 1)
  // 拖出窗口时 depth 可能残留（部分浏览器不补发 dragleave），用坐标兜底
  if (depth === 0 || e.clientX <= 0 || e.clientY <= 0) reset()
}

function onDrop(e: DragEvent) {
  if (!active.value) return
  e.preventDefault()
  const files = Array.from(e.dataTransfer?.files || [])
  reset()
  if (files.length) emit('files', files)
}

function reset() {
  depth = 0
  active.value = false
}

/** 拖出浏览器窗口后松手：不重置的话提示会一直挂在页面上。 */
function onWindowDragEnd() {
  reset()
}

onMounted(() => {
  // 用捕获阶段挂在 document 上：整页任意位置（含表格、卡片内部）都能落到
  document.addEventListener('dragenter', onDragEnter, true)
  document.addEventListener('dragover', onDragOver, true)
  document.addEventListener('dragleave', onDragLeave, true)
  document.addEventListener('drop', onDrop, true)
  window.addEventListener('dragend', onWindowDragEnd)
})

onBeforeUnmount(() => {
  document.removeEventListener('dragenter', onDragEnter, true)
  document.removeEventListener('dragover', onDragOver, true)
  document.removeEventListener('dragleave', onDragLeave, true)
  document.removeEventListener('drop', onDrop, true)
  window.removeEventListener('dragend', onWindowDragEnd)
})
</script>

<template>
  <!-- 整页落点提示：拖入时才出现，不占平时版面 -->
  <div v-if="active" class="drop-veil">
    <div class="drop-veil__box">
      <n-icon :size="34"><cloud-upload-outline /></n-icon>
      <span class="drop-veil__text">松手即上传到这里</span>
    </div>
  </div>
</template>

<style scoped>
.drop-veil {
  position: fixed;
  /* 盖住侧栏、顶栏与内容区：整页都是落点，提示也该覆盖整页 */
  z-index: 50;
  display: grid;
  inset: 0;
  place-items: center;
  padding: 20px;
  /* 半透明遮罩走令牌，明暗两套自动跟随（不要写死色值） */
  background: var(--color-overlay);
  backdrop-filter: blur(2px);
  pointer-events: none;
}

.drop-veil__box {
  display: grid;
  gap: 12px;
  justify-items: center;
  max-width: 100%;
  padding: 28px 40px;
  border: 2px dashed var(--color-primary);
  border-radius: var(--radius-card);
  color: var(--color-primary);
  background: var(--color-surface);
  box-shadow: var(--shadow-float);
}

.drop-veil__text {
  color: var(--color-text-strong);
  font-size: 16px;
  font-weight: 650;
  text-align: center;
  word-break: break-all;
}
</style>

<script setup lang="ts">
// 预览弹层：列表内点「预览」即在此打开，不再新开标签页。
//
// 为什么用 iframe（同源真实文档）而不是把预览组件直接挂进弹层：
//   - 弹层里不许出现预览页自己的任何 chrome（文件名、下载按钮那条栏已按要求删掉），
//     直接复用 /preview/:id 这条路由最省事，也不会把预览库拉进主包；
//   - docx/xlsx/pptx 三个预览库都会给自己「排版」到容器宽度，放进 iframe
//     等于给它们一个独立的视口，重排不会牵连主页面。
//
// 关键取舍：iframe 的 src 指向**同源真实路由**，而不是 srcdoc + Teleport。
// srcdoc 里手动搬 Naive UI 的样式试过：内容能渲染，但 Teleport 出去的部分
// 拿不到 Naive 的 CSS 变量作用域，深色档下提示文字仍是黑色。真同源文档会自己
// 跑一遍 SPA 启动流程，Naive 与明暗主题天然生效。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { NButton, NIcon, NModal } from 'naive-ui'
import { CloseOutline } from '@vicons/ionicons5'
import { closePreview, previewState } from '@/stores/preview'

/** 打开的文件 id；0 表示关闭。 */
const activeId = computed(() => previewState.id)

const show = computed({
  get: () => previewState.id > 0,
  set: (v: boolean) => {
    if (!v) closePreview()
  }
})

/**
 * 预览地址：带 embed=1 告诉预览页「自己被嵌在弹层里」，
 * 它据此不再渲染返回键等自身 chrome。
 */
const src = computed(() => (activeId.value > 0 ? `/preview/${activeId.value}?embed=1` : ''))

const frame = ref<HTMLIFrameElement | null>(null)

/**
 * iframe 内部也会处理 Esc（同源，能直接调 store），
 * 但焦点在 iframe 上时外层收不到按键，所以两边都监听、以先到的为准。
 */
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && show.value) closePreview()
}

onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))

// 关闭后清空 src：不保留上一个文件的解码内容与 blob。
watch(show, (v) => {
  if (!v) frame.value?.setAttribute('src', 'about:blank')
})
</script>

<template>
  <n-modal
    v-model:show="show"
    :mask-closable="true"
    :auto-focus="false"
    :trap-focus="false"
    :bordered="false"
    content-style="padding:0"
    @mask-click="closePreview"
    @esc="closePreview"
  >
    <div class="preview-shell">
      <iframe
        v-if="show"
        :key="activeId"
        ref="frame"
        :src="src"
        class="preview-frame"
        title="文件预览"
      />
      <!-- 关闭键悬浮在右上角：不占位置、不遮挡内容，也不用给预览面加一条标题栏。 -->
      <n-button
        class="preview-close"
        quaternary
        circle
        size="small"
        aria-label="关闭预览"
        @click="closePreview"
      >
        <template #icon>
          <n-icon><close-outline /></n-icon>
        </template>
      </n-button>
    </div>
  </n-modal>
</template>

<style scoped>
/* 弹层铺满视口：预览是「看内容」，留一圈卡片边与灰底只是白占地方。 */
.preview-shell {
  position: relative;
  width: 92vw;
  height: 92vh;
  overflow: hidden;
  border-radius: var(--radius-card);
  background: var(--color-surface);
  box-shadow: var(--shadow-float);
}

.preview-frame {
  display: block;
  width: 100%;
  height: 100%;
  border: 0;
  background: var(--color-surface);
}

/* 悬浮关闭键压在内容右上角，加一层半透明底保证任何内容上都看得见。 */
.preview-close {
  position: absolute;
  top: 8px;
  right: 8px;
  z-index: 2;
  background: var(--color-surface-translucent);
  box-shadow: var(--shadow-card);
}

@media (max-width: 768px) {
  /* 移动端不留边：屏幕本来就小，缩一圈只会让内容更难看清。 */
  .preview-shell {
    width: 100dvw;
    height: 100dvh;
    border-radius: 0;
  }
}
</style>

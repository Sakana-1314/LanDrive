<script setup lang="ts">
// xlsx 预览：用 exceljs 解析工作簿并自建轻量表格渲染。
//
// 出于性能考虑，只渲染前 MAX_ROWS 行 / MAX_COLS 列，超出时给出明确提示。
import { onMounted, ref, watch } from 'vue'
import { NAlert, NSpin, NTabs, NTabPane, NTag, NText } from 'naive-ui'

const MAX_ROWS = 2000
const MAX_COLS = 100

const props = defineProps<{ blob: Blob }>()

interface SheetData {
  name: string
  rows: string[][]
  totalRows: number
  totalCols: number
  truncated: boolean
}

const sheets = ref<SheetData[]>([])
const loading = ref(true)
const error = ref('')

/** 把 exceljs 单元格值转成展示字符串。 */
function cellText(v: unknown): string {
  if (v === null || v === undefined) return ''
  if (v instanceof Date) return v.toLocaleString()
  if (typeof v === 'object') {
    const o = v as Record<string, unknown>
    if ('richText' in o && Array.isArray(o.richText)) {
      return (o.richText as { text?: string }[]).map((r) => r.text ?? '').join('')
    }
    if ('text' in o) return String(o.text)
    if ('result' in o) return String(o.result ?? '')
    if ('hyperlink' in o) return String(o.text ?? o.hyperlink)
    if ('formula' in o) return String(o.result ?? '')
    return ''
  }
  return String(v)
}

async function parse() {
  loading.value = true
  error.value = ''
  sheets.value = []
  try {
    const ExcelJS = await import('exceljs')
    const wb = new ExcelJS.Workbook()
    const buf = await props.blob.arrayBuffer()
    await wb.xlsx.load(buf)
    const out: SheetData[] = []
    wb.eachSheet((ws) => {
      const totalRows = ws.actualRowCount || ws.rowCount || 0
      const totalCols = ws.actualColumnCount || ws.columnCount || 0
      const rowLimit = Math.min(totalRows, MAX_ROWS)
      const colLimit = Math.min(totalCols, MAX_COLS)
      const rows: string[][] = []
      for (let r = 1; r <= rowLimit; r++) {
        const row = ws.getRow(r)
        const cells: string[] = []
        for (let c = 1; c <= colLimit; c++) {
          cells.push(cellText(row.getCell(c).value))
        }
        rows.push(cells)
      }
      out.push({
        name: ws.name || `工作表${out.length + 1}`,
        rows,
        totalRows,
        totalCols,
        truncated: totalRows > MAX_ROWS || totalCols > MAX_COLS
      })
    })
    sheets.value = out
    if (out.length === 0) error.value = '该表格文件没有任何工作表'
  } catch (e) {
    error.value = `表格解析失败：${(e as Error).message}`
  } finally {
    loading.value = false
  }
}

const activeSheet = ref('')

onMounted(async () => {
  await parse()
  activeSheet.value = sheets.value[0]?.name || ''
})
watch(
  () => props.blob,
  async () => {
    await parse()
    activeSheet.value = sheets.value[0]?.name || ''
  }
)
</script>

<template>
  <div class="xlsx-wrap">
    <n-alert v-if="error" type="error" :title="error" />
    <n-spin v-else-if="loading" size="large" style="display: block; text-align: center; padding: 40px 0" />
    <template v-else>
      <n-tabs v-model:value="activeSheet" type="line" animated>
        <n-tab-pane v-for="s in sheets" :key="s.name" :name="s.name" :tab="s.name">
          <n-alert v-if="s.truncated" type="warning" style="margin-bottom: 8px">
            表格较大，仅渲染前 {{ MAX_ROWS }} 行 / {{ MAX_COLS }} 列（实际 {{ s.totalRows }} 行 ×
            {{ s.totalCols }} 列）。完整内容请下载后查看。
          </n-alert>
          <div class="sheet-scroll">
            <table class="sheet">
              <tbody>
                <tr v-for="(row, ri) in s.rows" :key="ri">
                  <th class="rowno">{{ ri + 1 }}</th>
                  <td v-for="(cell, ci) in row" :key="ci">{{ cell }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <n-text depth="3" style="font-size: 12px">
            共 {{ s.rows.length }} 行 × {{ s.rows[0]?.length || 0 }} 列
            <n-tag size="tiny" :bordered="false" style="margin-left: 6px">只读预览</n-tag>
          </n-text>
        </n-tab-pane>
      </n-tabs>
    </template>
  </div>
</template>

<style scoped>
.xlsx-wrap {
  background: #fff;
  padding: 12px;
  border-radius: 8px;
}
.sheet-scroll {
  max-height: calc(100vh - 230px);
  overflow: auto;
  border: 1px solid #e8e8e8;
  border-radius: 6px;
}
.sheet {
  border-collapse: collapse;
  font-size: 13px;
  white-space: nowrap;
}
.sheet th,
.sheet td {
  border: 1px solid #ededed;
  padding: 3px 8px;
  max-width: 320px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.sheet th.rowno {
  background: #f7f8fa;
  color: #999;
  font-weight: normal;
  text-align: right;
  position: sticky;
  left: 0;
  z-index: 1;
}
.sheet tbody tr:first-child td {
  font-weight: 600;
  background: #fafbfc;
}
</style>

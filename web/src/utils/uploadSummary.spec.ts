// 上传汇总口径的单元测试（Node 环境直接运行，零依赖）。
//
// 运行：node --experimental-strip-types src/utils/uploadSummary.spec.ts
//
// 守的是浮窗标题与总进度：这些数字最容易在改动中悄悄跑偏，
// 且跑偏了不会报错、只会显示一个"看着合理"的错值。
import assert from 'node:assert/strict'
import {
  panelTitle,
  summarizeUploads,
  summaryStatus,
  uploadStateLabel
} from './uploadSummary.ts'
import type { SummaryTask } from './uploadSummary.ts'

function task(p: Partial<SummaryTask>): SummaryTask {
  return { state: 'uploading', loaded: 0, total: 100, speed: 0, eta: 0, percent: 0, ...p }
}

// 1) 按字节折算：一个大文件传到一半，不能因为"任务数已过半"就报 100%
{
  const s = summarizeUploads([
    task({ state: 'done', total: 100, loaded: 100 }),
    task({ state: 'uploading', total: 900, loaded: 450 })
  ])
  assert.equal(s.percent, 55, `总进度应按字节折算（(100+450)/1000），实际 ${s.percent}`)
  assert.equal(s.active, 1)
  assert.equal(s.finished, 1)
  assert.equal(s.total, 2)
}

// 2) 全部完成 → 100%；分母为 0（空队列）→ 0%，不出现 NaN
assert.equal(summarizeUploads([task({ state: 'done', total: 10, loaded: 10 })]).percent, 100)
assert.equal(summarizeUploads([]).percent, 0)
assert.equal(summarizeUploads([]).percent === summarizeUploads([]).percent, true, '空队列不能是 NaN')

// 3) 速度只汇总"正在跑"的任务：已完成任务的残留速度会虚报带宽与剩余时间
{
  const s = summarizeUploads([
    task({ state: 'done', total: 100, loaded: 100, speed: 999 }),
    task({ state: 'uploading', total: 100, loaded: 0, speed: 10 }),
    task({ state: 'merging', total: 100, loaded: 100, speed: 5 })
  ])
  assert.equal(s.speed, 15, `速度应只汇总进行中的任务（10+5），实际 ${s.speed}`)
  // 剩余 = (100-0) + (100-100) = 100 字节，速度 15 → 约 6.67 秒
  assert.ok(Math.abs(s.eta - 100 / 15) < 1e-6, `剩余时间应按汇总速度折算，实际 ${s.eta}`)
}

// 4) 没有速度时不猜 ETA（0 而不是 Infinity）
assert.equal(summarizeUploads([task({ state: 'uploading', speed: 0 })]).eta, 0)

// 5) 标题的优先级：失败 > 进行中 > 已完成。传 20 个错 1 个时，
//    标题若是"已完成"用户根本不会展开看那一处失败。
{
  assert.equal(panelTitle(summarizeUploads([task({ state: 'error' })])), '1 个文件上传失败')
  assert.equal(
    panelTitle(
      summarizeUploads([
        task({ state: 'done', total: 10, loaded: 10 }),
        task({ state: 'done', total: 10, loaded: 10 }),
        task({ state: 'error', total: 10, loaded: 0 })
      ])
    ),
    '1 个文件上传失败'
  )
  assert.equal(panelTitle(summarizeUploads([task({ state: 'uploading' })])), '正在上传 1 个文件')
  assert.equal(
    panelTitle(summarizeUploads([task({ state: 'done', total: 10, loaded: 10 })])),
    '已完成 1 个上传'
  )
  assert.equal(panelTitle(summarizeUploads([])), '上传队列')
}

// 6) 取消算"已了结"，既不算进行中也不算失败（浮窗不该为一次主动取消报警）
{
  const s = summarizeUploads([task({ state: 'canceled', total: 100, loaded: 30 })])
  assert.equal(s.finished, 1)
  assert.equal(s.failed, 0)
  assert.equal(s.active, 0)
  assert.equal(s.percent, 100, '取消的任务按全部字节计入，否则总进度会卡在 30%')
}

// 7) 状态语义色：有失败即 error，全部结束即 success，否则 default
assert.equal(summaryStatus(summarizeUploads([task({ state: 'error' })])), 'error')
assert.equal(
  summaryStatus(summarizeUploads([task({ state: 'done', total: 1, loaded: 1 })])),
  'success'
)
assert.equal(summaryStatus(summarizeUploads([task({ state: 'uploading' })])), 'default')
assert.equal(summaryStatus(summarizeUploads([])), 'default')

// 8) 状态文案（含续传）
assert.equal(uploadStateLabel('uploading'), '上传中')
assert.equal(uploadStateLabel('uploading', true), '续传中')
assert.equal(uploadStateLabel('merging'), '合并中')
assert.equal(uploadStateLabel('canceled'), '已取消')

console.log('✅ 上传汇总测试全部通过（总进度按字节、速度只算进行中、标题优先级正确）')

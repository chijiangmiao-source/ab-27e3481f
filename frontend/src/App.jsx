import React, { useEffect, useMemo, useRef, useState, useCallback } from 'react'

const POSE_LABEL = {
  identity: '恒等（不旋转不镜像）',
  rot90: '顺时针 90°',
  rot180: '旋转 180°',
  rot270: '顺时针 270°',
  mirrorH: '水平镜像（上下翻转）',
  mirrorHrot90: '水平镜像 + 顺时针 90°',
  mirrorHrot180: '水平镜像 + 180°（左右翻转）',
  mirrorHrot270: '水平镜像 + 顺时针 270°'
}

// ---- 内置样例：同一批缺陷经 rot180 + 平移 (3,-5) 落在复检图 ----
function buildSample(n) {
  const cells = () => Array.from({ length: n * n }, () => 0)
  const ref = cells()
  const chk = cells()
  let seed = 20260920
  const rnd = () => {
    // 确定性 LCG，仅用于生成演示样例
    seed = (seed * 1103515245 + 12345) & 0x7fffffff
    return seed / 0x7fffffff
  }
  const used = new Set()
  let placed = 0
  while (placed < Math.floor(n * n * 0.05)) {
    const r = Math.floor(rnd() * n)
    const c = Math.floor(rnd() * n)
    const k = r * n + c
    if (used.has(k)) continue
    used.add(k)
    ref[k] = 1
    // chk 点经 rot180 后平移 (dy=3, dx=-5) 命中 ref(r,c)
    const qr = n - 1 - (r - 3)
    const qc = n - 1 - (c + 5)
    if (qr >= 0 && qr < n && qc >= 0 && qc < n) chk[qr * n + qc] = 1
    placed++
  }
  const toText = (m) => {
    const lines = []
    for (let r = 0; r < n; r++) lines.push(m.slice(r * n, r * n + n).join(''))
    return lines.join('\n')
  }
  return { reference: toText(ref), recheck: toText(chk) }
}

function OverlayCanvas({ overlay, n }) {
  const canvasRef = useRef(null)

  useEffect(() => {
    const cv = canvasRef.current
    if (!cv) return
    const dpr = window.devicePixelRatio || 1
    const cssSize = 512
    cv.width = cssSize * dpr
    cv.height = cssSize * dpr
    const ctx = cv.getContext('2d')
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
    ctx.fillStyle = '#0b0f17'
    ctx.fillRect(0, 0, cssSize, cssSize)

    const cell = cssSize / n
    // 小格（如 512 边长时每格 1px）满铺，避免稠密图看似稀疏；
    // 大格留 1px 细缝，便于分辨单点。
    const inset = cell >= 4 ? 1 : 0
    const side = Math.max(cell - 2 * inset, cell)
    for (let r = 0; r < n; r++) {
      const line = overlay[r]
      for (let c = 0; c < n; c++) {
        const ch = line[c]
        if (ch === '.') continue
        // X 重合用红蓝并列的证据色（紫/洋红），R 仅参考，B 仅复检
        ctx.fillStyle = ch === 'X' ? '#e044fb' : ch === 'R' ? '#ff4d4f' : '#3b9dff'
        ctx.fillRect(c * cell + (side < cell ? inset : 0), r * cell + (side < cell ? inset : 0),
          side < cell ? side : cell, side < cell ? side : cell)
      }
    }
  }, [overlay, n])

  return <canvas ref={canvasRef} style={{ width: 512, height: 512, imageRendering: 'pixelated' }} />
}

function ErrorBox({ errors, general }) {
  return (
    <div className="error-box" role="alert">
      <div className="error-title">请求失败，已清空旧结论</div>
      {general && <div className="error-line">{general}</div>}
      {errors && errors.length > 0 && (
        <ul className="error-list">
          {errors.map((e, i) => (
            <li key={i}>
              <span className={`tag tag-${e.field}`}>
                {e.field === 'reference' ? '参考图' : '复检图'}
              </span>
              {e.message}
              {e.line ? <span className="loc">（第 {e.line} 行{e.column ? ` 第 ${e.column} 列` : ''}）</span> : null}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

export default function App() {
  const [reference, setReference] = useState('')
  const [recheck, setRecheck] = useState('')
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState(null)
  const [errors, setErrors] = useState([])
  const [generalError, setGeneralError] = useState('')
  const [elapsed, setElapsed] = useState(null)

  const submit = useCallback(async () => {
    setLoading(true)
    // 发起新计算前先撤下旧结论：计算异常或校验失败绝不残留旧结果
    setResult(null)
    setErrors([])
    setGeneralError('')
    const t0 = performance.now()
    try {
      const resp = await fetch('/api/audit', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ reference, recheck })
      })
      const data = await resp.json().catch(() => null)
      if (!resp.ok) {
        if (data && data.errors) setErrors(data.errors)
        setGeneralError(data?.error || `服务返回 ${resp.status}`)
        return
      }
      setResult(data)
      setElapsed(performance.now() - t0)
    } catch (e) {
      setGeneralError('无法连接审计服务：' + e.message)
    } finally {
      setLoading(false)
    }
  }, [reference, recheck])

  const loadSample = () => {
    const s = buildSample(32)
    setReference(s.reference)
    setRecheck(s.recheck)
    setResult(null)
    setErrors([])
    setGeneralError('')
  }

  const clearAll = () => {
    setReference('')
    setRecheck('')
    setResult(null)
    setErrors([])
    setGeneralError('')
  }

  const fieldError = useMemo(() => {
    const m = { reference: null, recheck: null }
    for (const e of errors) if (m[e.field] === null) m[e.field] = e
    return m
  }, [errors])

  return (
    <div className="page">
      <header className="header">
        <h1>晶圆缺陷点阵 · 复检审计台</h1>
        <p className="sub">
          遍历正方形 8 种旋转/镜像姿态与纵移、横移各 [−N+1, N−1] 的整数平移，
          以精确整数二维相关（NTT，模 998244353，结果恒等）求最大重合，按
          <b> 姿态顺序 → 纵移 → 横移 </b>裁决规范解。
        </p>
      </header>

      <section className="inputs">
        <div className={`panel ${fieldError.reference ? 'panel-bad' : ''}`}>
          <div className="panel-head">
            <h2>参考图 A（基准点阵）</h2>
            <span className="hint">仅 0/1，每行等宽，边长 16~512</span>
          </div>
          <textarea
            spellCheck={false}
            value={reference}
            onChange={(e) => setReference(e.target.value)}
            placeholder={'0010...\n0100...\n…'}
          />
          {fieldError.reference && <div className="field-err">{fieldError.reference.message}</div>}
        </div>

        <div className={`panel ${fieldError.recheck ? 'panel-bad' : ''}`}>
          <div className="panel-head">
            <h2>复检图 B（旋转/镜像/平移后）</h2>
            <span className="hint">与参考图同边长；移出画布的缺陷仍计入总数</span>
          </div>
          <textarea
            spellCheck={false}
            value={recheck}
            onChange={(e) => setRecheck(e.target.value)}
            placeholder={'0010...\n0100...\n…'}
          />
          {fieldError.recheck && <div className="field-err">{fieldError.recheck.message}</div>}
        </div>
      </section>

      <section className="actions">
        <button className="primary" onClick={submit} disabled={loading}>
          {loading ? '审计计算中…' : '提交审计'}
        </button>
        <button onClick={loadSample} disabled={loading}>载入演示样例（rot180 + 平移）</button>
        <button onClick={clearAll} disabled={loading}>清空</button>
        <span className="action-note">输入修正后直接再次提交即可原样重试</span>
      </section>

      {(generalError || errors.length > 0) && (
        <ErrorBox errors={errors} general={generalError} />
      )}

      {result && <ResultView result={result} elapsed={elapsed} />}

      <footer className="footer">
        后端 Gin · 手写 NTT 精确整数相关（无浮点、无现成配准接口） · 前端 React
      </footer>
    </div>
  )
}

function ResultView({ result, elapsed }) {
  const c = result.canonical
  return (
    <section className="result">
      <h2>审计结论</h2>
      <div className="stat-grid">
        <div className="stat stat-hl">
          <div className="stat-k">规范变换</div>
          <div className="stat-v">
            {c.pose}
            <span className="stat-sub">{POSE_LABEL[c.pose] || c.pose}</span>
          </div>
        </div>
        <div className="stat stat-hl">
          <div className="stat-k">规范平移 (纵移 dy, 横移 dx)</div>
          <div className="stat-v">
            ({c.dy}, {c.dx})
            <span className="stat-sub">正方向：向下 / 向右</span>
          </div>
        </div>
        <div className="stat">
          <div className="stat-k">最大重合缺陷数</div>
          <div className="stat-v big">{result.maxOverlap}</div>
        </div>
        <div className="stat">
          <div className="stat-k">并列最优数量</div>
          <div className="stat-v big">{result.tieCount}</div>
        </div>
        <div className="stat">
          <div className="stat-k">参考图缺陷总数</div>
          <div className="stat-v big">{result.referenceTotal}</div>
        </div>
        <div className="stat">
          <div className="stat-k">复检图缺陷总数</div>
          <div className="stat-v big">{result.recheckTotal}</div>
        </div>
        <div className="stat">
          <div className="stat-k">规范解下移出画布的复检缺陷</div>
          <div className="stat-v big">{result.outsideCanvas}</div>
        </div>
        <div className="stat">
          <div className="stat-k">遍历空间</div>
          <div className="stat-v">
            {result.searchSpace.poses} × {result.searchSpace.shiftsPerPose.toLocaleString()}
            <span className="stat-sub">
              共 {result.searchSpace.totalTriples.toLocaleString()} 个（姿态,纵移,横移）三元组
            </span>
          </div>
        </div>
        <div className="stat">
          <div className="stat-k">方阵边长 N / 耗时</div>
          <div className="stat-v">
            {result.n}
            <span className="stat-sub">{elapsed != null ? `${elapsed.toFixed(0)} ms（含网络）` : ''}</span>
          </div>
        </div>
      </div>

      <div className="evidence">
        <div>
          <h3>红蓝叠加证据</h3>
          <div className="legend">
            <span><i className="sw" style={{ background: '#ff4d4f' }} />仅参考图</span>
            <span><i className="sw" style={{ background: '#3b9dff' }} />仅复检图</span>
            <span><i className="sw" style={{ background: '#e044fb' }} />重合（红+蓝）</span>
          </div>
          <OverlayCanvas overlay={result.overlay} n={result.n} />
        </div>
        <div>
          <h3>八姿态各自最优（固定姿态顺序裁决）</h3>
          <table className="pose-table">
            <thead>
              <tr><th>#</th><th>姿态</th><th>dy</th><th>dx</th><th>该姿态最大重合</th></tr>
            </thead>
            <tbody>
              {result.poseBests.map((p) => (
                <tr key={p.poseIndex} className={p.poseIndex === c.poseIndex ? 'row-win' : ''}>
                  <td>{p.poseIndex}</td>
                  <td>{p.pose}</td>
                  <td>{p.dy}</td>
                  <td>{p.dx}</td>
                  <td>{p.score}</td>
                </tr>
              ))}
            </tbody>
          </table>
          <p className="note">
            规范解在全局最大值并列时取姿态编号最小、再取纵移最小、再取横移最小者；
            并列最优数量统计达到同一全局最大值的全部三元组。
          </p>
        </div>
      </div>
    </section>
  )
}

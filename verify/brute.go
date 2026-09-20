package main

// brute.go —— 验收侧独立实现的八姿态变换与 O(n^4) 暴力遍历。
//
// 刻意不依赖后端任何代码：姿态映射、平移遍历、重合计数、规范裁决全部在此
// 重写一遍，用于交叉核对真实 Gin API 返回的结论。

const poseCount = 8

var poseNames = [poseCount]string{
	"identity", "rot90", "rot180", "rot270",
	"mirrorH", "mirrorHrot90", "mirrorHrot180", "mirrorHrot270",
}

// xform 独立给出姿态 p 下 (r,c) 的映射，定义与服务约定相同。
func xform(p, r, c, n int) (int, int) {
	switch p {
	case 0:
		return r, c
	case 1:
		return c, n - 1 - r
	case 2:
		return n - 1 - r, n - 1 - c
	case 3:
		return n - 1 - c, r
	case 4:
		return n - 1 - r, c
	case 5:
		return c, r
	case 6:
		return r, n - 1 - c
	default:
		return n - 1 - c, n - 1 - r
	}
}

// cand 一个（姿态,纵移,横移）候选的暴力结果。
type cand struct {
	pose    int
	dy, dx  int
	overlap int
}

// bruteSolution 遍历全部 8*(2n-1)^2 个三元组，返回全部候选（按规范顺序）、
// 最大重合、并列数与规范解。
type bruteSolution struct {
	all       []cand
	max       int
	tie       int
	canonical cand
}

func brute(ref, chk []byte, n int) bruteSolution {
	var all []cand
	best := -1
	for p := 0; p < poseCount; p++ {
		bp := make([]byte, n*n)
		for r := 0; r < n; r++ {
			for c := 0; c < n; c++ {
				nr, nc := xform(p, r, c, n)
				bp[nr*n+nc] = chk[r*n+c]
			}
		}
		for dy := -(n - 1); dy <= n-1; dy++ {
			for dx := -(n - 1); dx <= n-1; dx++ {
				ov := 0
				for r := 0; r < n; r++ {
					for c := 0; c < n; c++ {
						if bp[r*n+c] == 0 {
							continue
						}
						tr, tc := r+dy, c+dx
						if tr >= 0 && tr < n && tc >= 0 && tc < n && ref[tr*n+tc] == 1 {
							ov++
						}
					}
				}
				all = append(all, cand{p, dy, dx, ov})
				if ov > best {
					best = ov
				}
			}
		}
	}
	sol := bruteSolution{all: all, max: best}
	first := true
	for _, q := range all { // all 已按 pose,dy,dx 顺序追加
		if q.overlap == best {
			sol.tie++
			if first {
				sol.canonical = q
				first = false
			}
		}
	}
	return sol
}

// bruteOverlay 按规范解独立重建红蓝叠加（'.' 'R' 'B' 'X'）。
func bruteOverlay(ref, chk []byte, n int, win cand) []string {
	bp := make([]byte, n*n)
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			nr, nc := xform(win.pose, r, c, n)
			bp[nr*n+nc] = chk[r*n+c]
		}
	}
	grid := make([][]byte, n)
	for r := 0; r < n; r++ {
		grid[r] = make([]byte, n)
		for c := 0; c < n; c++ {
			if ref[r*n+c] == 1 {
				grid[r][c] = 'R'
			} else {
				grid[r][c] = '.'
			}
		}
	}
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if bp[r*n+c] == 0 {
				continue
			}
			tr, tc := r+win.dy, c+win.dx
			if tr < 0 || tr >= n || tc < 0 || tc >= n {
				continue
			}
			if ref[tr*n+tc] == 1 {
				grid[tr][tc] = 'X'
			} else {
				grid[tr][tc] = 'B'
			}
		}
	}
	out := make([]string, n)
	for r := range out {
		out[r] = string(grid[r])
	}
	return out
}

package audit

import (
	"math/rand"
	"testing"
)

// bruteForce 用最朴素的四重循环独立计算每个 (姿态,dy,dx) 的画布内重合数，
// 作为 NTT 结果的交叉核对基准，并独立给出规范解与并列数。
func bruteForce(t *testing.T, ref, chk []byte, n int) (maxOverlap, tie int, pose int, dy, dx int) {
	type cand struct {
		p, dy, dx, s int
	}
	var all []cand
	best := -1
	for p := 0; p < PoseCount; p++ {
		bp := applyPose(p, chk, n)
		for ddy := -(n - 1); ddy <= n-1; ddy++ {
			for ddx := -(n - 1); ddx <= n-1; ddx++ {
				s := 0
				for r := 0; r < n; r++ {
					for c := 0; c < n; c++ {
						if bp[r*n+c] == 0 {
							continue
						}
						tr, tc := r+ddy, c+ddx
						if tr >= 0 && tr < n && tc >= 0 && tc < n && ref[tr*n+tc] == 1 {
							s++
						}
					}
				}
				all = append(all, cand{p, ddy, ddx, s})
				if s > best {
					best = s
				}
			}
		}
	}
	first := true
	for _, q := range all { // all 已按 pose,dy,dx 顺序追加
		if q.s == best {
			tie++
			if first {
				pose, dy, dx = q.p, q.dy, q.dx
				first = false
			}
		}
	}
	return best, tie, pose, dy, dx
}

func TestRandomAgainstBruteForce(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for trial := 0; trial < 40; trial++ {
		n := 16 + rng.Intn(40) // 16..55，暴力可承受
		ref := make([]byte, n*n)
		chk := make([]byte, n*n)
		density := []float64{0.02, 0.1, 0.4, 0.8}[rng.Intn(4)]
		for i := range ref {
			if rng.Float64() < density {
				ref[i] = 1
			}
			if rng.Float64() < density {
				chk[i] = 1
			}
		}
		res := Audit(ref, chk, n)
		best, tie, p, bdy, bdx := bruteForce(t, ref, chk, n)
		if res.MaxOverlap != best {
			t.Fatalf("trial %d n=%d: maxOverlap NTT=%d brute=%d", trial, n, res.MaxOverlap, best)
		}
		if res.TieCount != tie {
			t.Fatalf("trial %d n=%d: tie NTT=%d brute=%d", trial, n, res.TieCount, tie)
		}
		if res.Canonical.PoseIndex != p || res.Canonical.DY != bdy || res.Canonical.DX != bdx {
			t.Fatalf("trial %d: canonical NTT=(%d,%d,%d) brute=(%d,%d,%d)",
				trial, res.Canonical.PoseIndex, res.Canonical.DY, res.Canonical.DX, p, bdy, bdx)
		}
	}
}

// TestKnownTransform 构造“同一批缺陷经 rot180 + 平移 (3,-5)”得到的复检图，
// 规范解必须精确还原该变换（且存在唯一并列）。
func TestKnownTransform(t *testing.T) {
	n := 64
	ref := make([]byte, n*n)
	rng := rand.New(rand.NewSource(7))
	pts := []struct{ r, c int }{}
	for len(pts) < 40 {
		r, c := rng.Intn(n), rng.Intn(n)
		if ref[r*n+c] == 0 {
			ref[r*n+c] = 1
			pts = append(pts, struct{ r, c int }{r, c})
		}
	}
	// 复检图 = ref 经姿态 rot180(=2) 后整体平移 (3,-5) 落在画布上的部分；
	// 即 chk 中一点经 rot180 再平移与 ref 重合。为使最大重合唯一，
	// 额外缺陷密度低、点位置分散。
	chk := make([]byte, n*n)
	const ddy, ddx = 3, -5
	for _, pt := range pts {
		// 要求 chk 点经 rot180 再平移 (ddy,ddx) 后落在 ref 点 (r,c)：
		// (n-1-qr+ddy, n-1-qc+ddx) = (r,c)
		qr := n - 1 - (pt.r - ddy)
		qc := n - 1 - (pt.c - ddx)
		if qr >= 0 && qr < n && qc >= 0 && qc < n {
			chk[qr*n+qc] = 1
		}
	}
	res := Audit(ref, chk, n)
	if res.Canonical.PoseIndex != 2 {
		t.Fatalf("pose = %d (%s), want 2 (rot180)", res.Canonical.PoseIndex, res.Canonical.Pose)
	}
	if res.Canonical.DY != ddy || res.Canonical.DX != ddx {
		t.Fatalf("shift = (%d,%d), want (%d,%d)", res.Canonical.DY, res.Canonical.DX, ddy, ddx)
	}
}

// TestParseErrors 定位校验：非法字符与行宽。
func TestParseErrors(t *testing.T) {
	mk := func(n int, mutate func([]string)) string {
		rows := make([]string, n)
		for i := range rows {
			b := make([]byte, n)
			for j := range b {
				b[j] = '0'
			}
			rows[i] = string(b)
		}
		mutate(rows)
		out := ""
		for _, r := range rows {
			out += r + "\n"
		}
		return out
	}

	t.Run("illegal char on recheck", func(t *testing.T) {
		txt := mk(16, func(rows []string) {
			b := []byte(rows[3])
			b[7] = '2'
			rows[3] = string(b)
		})
		_, _, fe := ParseMatrix("recheck", txt)
		if fe == nil || fe.Field != "recheck" || fe.Code != "illegal_char" || fe.Line != 4 || fe.Column != 8 {
			t.Fatalf("got %+v", fe)
		}
	})

	t.Run("ragged width", func(t *testing.T) {
		txt := mk(16, func(rows []string) { rows[5] = rows[5][:10] })
		_, _, fe := ParseMatrix("reference", txt)
		if fe == nil || fe.Code != "ragged_width" || fe.Line != 6 {
			t.Fatalf("got %+v", fe)
		}
	})

	t.Run("side out of range", func(t *testing.T) {
		_, _, fe := ParseMatrix("reference", "0101\n1010\n0101\n1010\n")
		if fe == nil || fe.Code != "side_out_of_range" {
			t.Fatalf("got %+v", fe)
		}
	})

	t.Run("not square", func(t *testing.T) {
		txt := mk(16, func(rows []string) { rows = append(rows, rows[0]) })
		// 上面的 append 无法传出，直接构造 17 行文本
		_ = txt
		var s string
		for i := 0; i < 17; i++ {
			for j := 0; j < 16; j++ {
				s += "0"
			}
			s += "\n"
		}
		_, _, fe := ParseMatrix("reference", s)
		if fe == nil || fe.Code != "not_square" {
			t.Fatalf("got %+v", fe)
		}
	})
}

// TestFullDense512 稠密满尺寸：全 1 矩阵重合面积为 (n-|dy|)(n-|dx|)，
// 最大值 n*n 仅在 (0,0) 达到，八姿态等价故 tie=8；主要验证 512 满尺寸
// 快速完成（NTT O(n^2 log n)，而非逐点尝试全部平移）。
func TestFullDense512(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	n := 512
	a := make([]byte, n*n)
	b := make([]byte, n*n)
	for i := range a {
		a[i] = 1
		b[i] = 1
	}
	res := Audit(a, b, n)
	if res.TieCount != 8 {
		t.Fatalf("dense tie=%d want 8", res.TieCount)
	}
	if res.MaxOverlap != n*n {
		t.Fatalf("maxOverlap=%d want %d", res.MaxOverlap, n*n)
	}
	if res.Canonical.PoseIndex != 0 || res.Canonical.DY != 0 || res.Canonical.DX != 0 {
		t.Fatalf("canonical=%+v want pose0 dy=dx=0", res.Canonical)
	}
	if res.OutsideCanvas != 0 {
		t.Fatalf("outside=%d want 0", res.OutsideCanvas)
	}
}

// TestEmpty512 全空方阵：每个 (姿态,dy,dx) 的重合数都是 0，全部并列，
// tieCount 必须恰为 8*(2n-1)^2，规范解按姿态、纵移、横移取第一个。
func TestEmpty512(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	n := 512
	a := make([]byte, n*n)
	b := make([]byte, n*n)
	res := Audit(a, b, n)
	wantTie := 8 * (2*n - 1) * (2*n - 1)
	if res.TieCount != wantTie {
		t.Fatalf("empty tie=%d want %d", res.TieCount, wantTie)
	}
	if res.MaxOverlap != 0 {
		t.Fatalf("maxOverlap=%d want 0", res.MaxOverlap)
	}
	if res.Canonical.PoseIndex != 0 || res.Canonical.DY != -(n-1) || res.Canonical.DX != -(n-1) {
		t.Fatalf("canonical=%+v want pose0 dy=dx=%d", res.Canonical, -(n - 1))
	}
}

package audit

// ntt.go —— 精确整数二维相关
//
// 重合计数是两个 0/1 矩阵的互相关，取值全部为非负整数。为了让稠密的满尺寸
// 输入（512×512）也不必逐点尝试全部平移，这里用模 998244353 的数论变换
// （NTT）做卷积：998244353 = 119*2^23+1 是 NTT 友好素数，而任意位置的重合
// 计数至多为 n^2 ≤ 262144，远小于模数，故模运算结果与真实整数完全一致，
// 不存在浮点配准选错对称姿态的问题。
//
// 二维卷积通过“逐行一维 NTT + 逐列一维 NTT”完成，复杂度 O(n^2 log n)；
// 参考图的二维频谱只计算一次、在八个姿态间复用。全程不调用任何现成配准或
// 二维相关接口，NTT 与蝶形运算均为本文件内的手写实现。

import "sync"

const (
	nttMod  = 998244353
	nttRoot = 3 // 模 998244353 的原根
)

func nttModPow(a, e int) int {
	r := 1
	for e > 0 {
		if e&1 == 1 {
			r = r * a % nttMod
		}
		a = a * a % nttMod
		e >>= 1
	}
	return r
}

// twiddleCache 缓存每种变换长度（正/逆）各级的旋转因子，避免重复求幂。
type twiddleKey struct {
	length  int
	inverse bool
}

var twiddleCache sync.Map

func twiddles(length int, inverse bool) [][]int {
	key := twiddleKey{length, inverse}
	if v, ok := twiddleCache.Load(key); ok {
		return v.([][]int)
	}
	stages := 0
	for l := length; l > 1; l >>= 1 {
		stages++
	}
	tbl := make([][]int, stages)
	idx := 0
	for sz := 2; sz <= length; sz <<= 1 {
		wlen := nttModPow(nttRoot, (nttMod-1)/sz)
		if inverse {
			wlen = nttModPow(wlen, nttMod-2)
		}
		half := sz >> 1
		ws := make([]int, half)
		ws[0] = 1
		for j := 1; j < half; j++ {
			ws[j] = ws[j-1] * wlen % nttMod
		}
		tbl[idx] = ws
		idx++
	}
	twiddleCache.Store(key, tbl)
	return tbl
}

// ntt 对长度必为 2 的幂的 a 做原地数论变换；inverse=true 时为逆变换。
func ntt(a []int, inverse bool) {
	n := len(a)

	// 位逆序置换
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for j&bit != 0 {
			j ^= bit
			bit >>= 1
		}
		j ^= bit
		if i < j {
			a[i], a[j] = a[j], a[i]
		}
	}

	tbl := twiddles(n, inverse)
	stage := 0
	for length := 2; length <= n; length <<= 1 {
		ws := tbl[stage]
		stage++
		half := length >> 1
		for start := 0; start < n; start += length {
			for j := 0; j < half; j++ {
				u := a[start+j]
				v := a[start+j+half] * ws[j] % nttMod
				a[start+j] = u + v
				if a[start+j] >= nttMod {
					a[start+j] -= nttMod
				}
				a[start+j+half] = u - v
				if a[start+j+half] < 0 {
					a[start+j+half] += nttMod
				}
			}
		}
	}

	if inverse {
		invN := nttModPow(n, nttMod-2)
		for i := range a {
			a[i] = a[i] * invN % nttMod
		}
	}
}

// nextPow2 返回不小于 x 的最小 2 的幂。
func nextPow2(x int) int {
	s := 1
	for s < x {
		s <<= 1
	}
	return s
}

// ntt2D 对 R 行 C 列（行列均为 2 的幂）的行主序矩阵做原地二维 NTT：
// 先逐行变换，再逐列变换。
func ntt2D(m []int, r, c int, inverse bool) {
	if inverse {
		// 逆变换顺序与正变换相反：先行后列的逆序是 先列后行。
		col := make([]int, r)
		for j := 0; j < c; j++ {
			for i := 0; i < r; i++ {
				col[i] = m[i*c+j]
			}
			ntt(col, true)
			for i := 0; i < r; i++ {
				m[i*c+j] = col[i]
			}
		}
		for i := 0; i < r; i++ {
			ntt(m[i*c:(i+1)*c], true)
		}
		return
	}
	for i := 0; i < r; i++ {
		ntt(m[i*c:(i+1)*c], false)
	}
	col := make([]int, r)
	for j := 0; j < c; j++ {
		for i := 0; i < r; i++ {
			col[i] = m[i*c+j]
		}
		ntt(col, false)
		for i := 0; i < r; i++ {
			m[i*c+j] = col[i]
		}
	}
}

// correlator 持有边长 n 的两张方阵相关所需的填充尺寸与参考图频谱。
//
// 线性相关的输出边长为 2n-1，故两个方向都填充到不小于 2n-1 的 2 的幂 L；
// 频域逐点相乘后，循环卷积的前 2n-1 个输出即完整线性卷积。
type correlator struct {
	n      int
	l      int
	refFreq []int // 参考图 A 的二维 NTT 频谱（L×L）
}

func newCorrelator(ref []byte, n int) *correlator {
	l := nextPow2(2*n - 1)
	fa := make([]int, l*l)
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			fa[r*l+c] = int(ref[r*n+c])
		}
	}
	ntt2D(fa, l, l, false)
	return &correlator{n: n, l: l, refFreq: fa}
}

// crossCorrelate 计算 A 与变换后的复检图 bp（边长 n，已做姿态变换）的
// 完整二维互相关，返回扁平的 (2n-1)×(2n-1) 矩阵（行主序）。
//
// 结果 corr[t][l] = Σ_i Σ_j A[i][j] * bp[i-(t-(n-1))][j-(l-(n-1))]，
// 即 bp 平移 (dy,dx)=(t-(n-1), l-(n-1)) 后与 A 的画布内重合缺陷数。
func (q *correlator) crossCorrelate(bp []byte) []int {
	l := q.l
	fb := make([]int, l*l)

	// 互相关 = A 与“双向翻转的 bp”的卷积：把 bp 放到填充阵列的
	// (n-1, n-1) 偏移处并做 180° 翻转，使卷积下标 (n-1+dy, n-1+dx)
	// 恰好对应平移 (dy,dx) 的相关值。
	for r := 0; r < q.n; r++ {
		for c := 0; c < q.n; c++ {
			v := bp[(q.n-1-r)*q.n+(q.n-1-c)]
			if v != 0 {
				fb[r*l+c] = 1
			}
		}
	}
	ntt2D(fb, l, l, false)
	for i := range fb {
		fb[i] = fb[i] * q.refFreq[i] % nttMod
	}
	ntt2D(fb, l, l, true)

	outN := 2*q.n - 1
	out := make([]int, outN*outN)
	for t := 0; t < outN; t++ {
		for s := 0; s < outN; s++ {
			out[t*outN+s] = fb[t*l+s]
		}
	}
	return out
}

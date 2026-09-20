package audit

// solve.go —— 解析校验、八姿态遍历、规范裁决与叠加证据
//
// 规范解裁决顺序（题目固定）：姿态顺序（identity, rot90, ..., mirrorHrot270）
// 为第一优先级，纵移 dy 升序为第二优先级，横移 dx 升序为第三优先级。
// 扫描相关矩阵时严格按该顺序，首次遇到的全局最大值即规范解；
// 与全局最大值相等的 (姿态,dy,dx) 三元组总数即并列最优数量。

import (
	"fmt"
	"runtime"
	"sync"
)

// Side limits.
const (
	MinSide = 16
	MaxSide = 512
)

// FieldError 定位到具体输入框的校验错误。行、列从 1 开始计数。
type FieldError struct {
	Field   string `json:"field"`   // "reference" | "recheck"
	Code    string `json:"code"`    // 机器可读码
	Message string `json:"message"` // 中文说明
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
	Width   int    `json:"width,omitempty"`
	Expect  int    `json:"expect,omitempty"`
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ParseMatrix 解析二值方阵文本。
// 允许字符仅为 '0' '1' '\n'，'\r' 作为 CRLF 兼容被吞掉；
// 行内出现其它字符立即按行列定位报错；行宽不一致按行号报错；
// 最终行列数必须相等且边长在 [16,512]。
func ParseMatrix(field, text string) ([]byte, int, *FieldError) {
	if len(text) == 0 {
		return nil, 0, &FieldError{Field: field, Code: "empty", Message: "输入为空"}
	}

	var rows [][]byte
	var row []byte
	line := 1
	col := 0
	flush := func() {
		cp := make([]byte, len(row))
		copy(cp, row)
		rows = append(rows, cp)
		row = nil
	}

	for i := 0; i < len(text); i++ {
		ch := text[i]
		switch ch {
		case '0':
			row = append(row, 0)
			col++
		case '1':
			row = append(row, 1)
			col++
		case '\r':
			// 与 \n 配对的 CR 忽略
		case '\n':
			flush()
			line++
			col = 0
		default:
			return nil, 0, &FieldError{
				Field:   field,
				Code:    "illegal_char",
				Message:  fmt.Sprintf("第 %d 行第 %d 列出现非法字符 %q（仅允许 0、1 与换行）", line, col+1, string(ch)),
				Line:    line,
				Column:  col + 1,
			}
		}
	}
	// 末尾换行后没有残余内容时不补空行；否则补最后一行。
	if len(row) > 0 || (len(text) > 0 && text[len(text)-1] != '\n' && text[len(text)-1] != '\r') {
		flush()
	}

	if len(rows) == 0 {
		return nil, 0, &FieldError{Field: field, Code: "empty", Message: "输入为空或仅含换行符"}
	}

	width := len(rows[0])
	for i, r := range rows {
		if len(r) != width {
			return nil, 0, &FieldError{
				Field:   field,
				Code:    "ragged_width",
				Message:  fmt.Sprintf("第 %d 行宽度为 %d，与首行宽度 %d 不一致", i+1, len(r), width),
				Line:    i + 1,
				Width:   len(r),
				Expect:  width,
			}
		}
	}
	n := width
	rowsN := len(rows)
	if n < MinSide || n > MaxSide {
		return nil, 0, &FieldError{
			Field:   field,
			Code:    "side_out_of_range",
			Message:  fmt.Sprintf("方阵边长为 %d，超出允许范围 %d~%d", n, MinSide, MaxSide),
			Width:   n,
		}
	}
	if rowsN != n {
		return nil, 0, &FieldError{
			Field:   field,
			Code:    "not_square",
			Message:  fmt.Sprintf("不是方阵：共 %d 行、每行 %d 列", rowsN, n),
			Line:    rowsN,
			Width:   n,
			Expect:  n,
		}
	}

	flat := make([]byte, n*n)
	for i, r := range rows {
		copy(flat[i*n:], r)
	}
	return flat, n, nil
}

func countOnes(m []byte) int {
	c := 0
	for _, v := range m {
		c += int(v)
	}
	return c
}

// PoseBest 每个姿态自身的最优平移。
type PoseBest struct {
	PoseIndex int    `json:"poseIndex"`
	Pose      string `json:"pose"`
	DY        int    `json:"dy"`
	DX        int    `json:"dx"`
	Score     int    `json:"score"`
}

// Result 是审计计算的完整结论。
type Result struct {
	N              int        `json:"n"`
	ReferenceTotal int        `json:"referenceTotal"` // 参考图原图缺陷总数（不受平移影响）
	RecheckTotal   int        `json:"recheckTotal"`   // 复检图原图缺陷总数（移出画布者仍计入）
	MaxOverlap     int        `json:"maxOverlap"`     // 最大画布内重合缺陷数
	TieCount       int        `json:"tieCount"`       // 并列最优（姿态,纵移,横移）三元组数量
	Canonical      Transform  `json:"canonical"`
	OutsideCanvas  int        `json:"outsideCanvas"` // 规范解下复检缺陷移出画布的数量
	PoseBests      []PoseBest `json:"poseBests"`
	// Overlay 为 n 行字符串，每格：'.' 空、'R' 仅参考图、'B' 仅复检图、'X' 重合
	Overlay []string `json:"overlay"`
	// SearchSpace 明示遍历规模（频域相关一次给出全部平移，非逐点尝试）。
	SearchSpace SearchSpace `json:"searchSpace"`
}

// Transform 规范变换。
type Transform struct {
	PoseIndex int    `json:"poseIndex"`
	Pose      string `json:"pose"`
	DY        int    `json:"dy"`
	DX        int    `json:"dx"`
}

// SearchSpace 描述遍历空间。
type SearchSpace struct {
	Poses        int `json:"poses"`
	ShiftsPerPose int `json:"shiftsPerPose"` // (2n-1)^2
	TotalTriples  int `json:"totalTriples"`
}

// Audit 执行完整审计。ref/chk 为已解析的边长 n 的 0/1 扁平方阵。
func Audit(ref, chk []byte, n int) *Result {
	refTotal := countOnes(ref)
	chkTotal := countOnes(chk)

	corr := newCorrelator(ref, n)
	outN := 2*n - 1

	results := make([][][]int, PoseCount)
	bests := make([]PoseBest, PoseCount)

	// 八姿态并行；相关矩阵只与本姿态的变换图有关，参考图频谱共享只读。
	workers := runtime.NumCPU()
	if workers > PoseCount {
		workers = PoseCount
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for p := 0; p < PoseCount; p++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(p int) {
			defer wg.Done()
			defer func() { <-sem }()
			bp := applyPose(p, chk, n)
			flat := corr.crossCorrelate(bp)
			grid := make([][]int, outN)
			for t := 0; t < outN; t++ {
				grid[t] = flat[t*outN : (t+1)*outN]
			}
			results[p] = grid

			// 严格按 纵移升序、横移升序 找该姿态首个最大值。
			bestScore := -1
			bestDY, bestDX := 0, 0
			for t := 0; t < outN; t++ {
				for s := 0; s < outN; s++ {
					if grid[t][s] > bestScore {
						bestScore = grid[t][s]
						bestDY = t - (n - 1)
						bestDX = s - (n - 1)
					}
				}
			}
			bests[p] = PoseBest{
				PoseIndex: p,
				Pose:      PoseName[p],
				DY:        bestDY,
				DX:        bestDX,
				Score:     bestScore,
			}
		}(p)
	}
	wg.Wait()

	// 全局最大值：姿态升序优先（bests 已按下标序写入）。
	globalMax := -1
	winnerPose := 0
	for p := 0; p < PoseCount; p++ {
		if bests[p].Score > globalMax {
			globalMax = bests[p].Score
			winnerPose = p
		}
	}
	winner := bests[winnerPose]

	// 并列最优计数：同序二次扫描全部三元组。
	tieCount := 0
	for p := 0; p < PoseCount; p++ {
		for t := 0; t < outN; t++ {
			row := results[p][t]
			for s := 0; s < outN; s++ {
				if row[s] == globalMax {
					tieCount++
				}
			}
		}
	}

	// 规范解下的红蓝叠加证据。
	bp := applyPose(winnerPose, chk, n)
	overlay := make([]string, n)
	insideMoved := 0
	for r := 0; r < n; r++ {
		buf := make([]byte, n)
		for c := 0; c < n; c++ {
			buf[c] = '.'
			if ref[r*n+c] == 1 {
				buf[c] = 'R'
			}
		}
		overlay[r] = string(buf)
	}
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if bp[r*n+c] == 0 {
				continue
			}
			tr := r + winner.DY
			tc := c + winner.DX
			if tr < 0 || tr >= n || tc < 0 || tc >= n {
				continue // 移出画布：不计叠加，但仍在复检原图总数内
			}
			insideMoved++
			buf := []byte(overlay[tr])
			if ref[tr*n+tc] == 1 {
				buf[tc] = 'X'
			} else {
				buf[tc] = 'B'
			}
			overlay[tr] = string(buf)
		}
	}

	shiftCount := outN * outN
	return &Result{
		N:              n,
		ReferenceTotal: refTotal,
		RecheckTotal:   chkTotal,
		MaxOverlap:     globalMax,
		TieCount:       tieCount,
		Canonical: Transform{
			PoseIndex: winner.PoseIndex,
			Pose:      winner.Pose,
			DY:        winner.DY,
			DX:        winner.DX,
		},
		OutsideCanvas: chkTotal - insideMoved,
		PoseBests:     bests,
		Overlay:       overlay,
		SearchSpace: SearchSpace{
			Poses:         PoseCount,
			ShiftsPerPose: shiftCount,
			TotalTriples:  PoseCount * shiftCount,
		},
	}
}

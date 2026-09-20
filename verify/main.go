package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"time"
)

// apiResult /api/audit 成功响应（仅取需要核对的字段）。
type apiResult struct {
	N              int `json:"n"`
	ReferenceTotal int `json:"referenceTotal"`
	RecheckTotal   int `json:"recheckTotal"`
	MaxOverlap     int `json:"maxOverlap"`
	TieCount       int `json:"tieCount"`
	OutsideCanvas  int `json:"outsideCanvas"`
	Overlay        []string `json:"overlay"`
	Canonical      struct {
		PoseIndex int    `json:"poseIndex"`
		Pose      string `json:"pose"`
		DY        int    `json:"dy"`
		DX        int    `json:"dx"`
	} `json:"canonical"`
	PoseBests []struct {
		PoseIndex int `json:"poseIndex"`
		DY        int `json:"dy"`
		DX        int `json:"dx"`
		Score     int `json:"score"`
	} `json:"poseBests"`
	SearchSpace struct {
		Poses          int `json:"poses"`
		ShiftsPerPose  int `json:"shiftsPerPose"`
		TotalTriples   int `json:"totalTriples"`
	} `json:"searchSpace"`
}

type apiError struct {
	Error  string `json:"error"`
	Errors []struct {
		Field   string `json:"field"`
		Code    string `json:"code"`
		Line    int    `json:"line"`
		Column  int    `json:"column"`
		Message string `json:"message"`
	} `json:"errors"`
}

type checker struct {
	client *http.Client
	base   string
	failed int
	passed int
}

func main() {
	base := os.Getenv("BACKEND_URL")
	if base == "" {
		base = "http://backend:8080"
	}
	ck := &checker{
		client: &http.Client{Timeout: 60 * time.Second},
		base:   base,
	}

	ck.waitForBackend(60 * time.Second)
	ck.checkHealth()
	ck.checkMeta()
	ck.checkRandomCrossCheck()
	ck.checkKnownTransform()
	ck.checkOutsideCanvas()
	ck.checkDense512()
	ck.checkValidation()
	ck.checkRetryAfterFix()

	fmt.Println("----------------------------------------")
	fmt.Printf("验收完成：%d 通过, %d 失败\n", ck.passed, ck.failed)
	if ck.failed > 0 {
		os.Exit(1)
	}
}

func (ck *checker) ok(name string, cond bool, detail ...string) {
	if cond {
		ck.passed++
		fmt.Printf("  [PASS] %s\n", name)
		return
	}
	ck.failed++
	fmt.Printf("  [FAIL] %s", name)
	for _, d := range detail {
		fmt.Printf(" — %s", d)
	}
	fmt.Println()
}

func (ck *checker) get(path string) ([]byte, int) {
	resp, err := ck.client.Get(ck.base + path)
	if err != nil {
		return nil, -1
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return b, resp.StatusCode
}

func (ck *checker) audit(ref, chk string) (*apiResult, *apiError, int) {
	body, _ := json.Marshal(map[string]string{"reference": ref, "recheck": chk})
	resp, err := ck.client.Post(ck.base+"/api/audit", "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Printf("  请求异常: %v\n", err)
		return nil, nil, -1
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 200 {
		var r apiResult
		if err := json.Unmarshal(raw, &r); err != nil {
			fmt.Printf("  响应解析异常: %v body=%s\n", err, string(raw))
			return nil, nil, resp.StatusCode
		}
		return &r, nil, 200
	}
	var e apiError
	_ = json.Unmarshal(raw, &e)
	return nil, &e, resp.StatusCode
}

func (ck *checker) waitForBackend(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(ck.base + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				fmt.Printf("后端已就绪：%s\n\n", ck.base)
				return
			}
		}
		time.Sleep(1 * time.Second)
	}
	fmt.Println("等待后端健康检查超时，继续尝试验收…")
}

func (ck *checker) checkHealth() {
	fmt.Println("● 健康检查 GET /healthz")
	b, code := ck.get("/healthz")
	ck.ok("后端 /healthz 返回 200", code == 200, fmt.Sprintf("code=%d body=%s", code, string(b)))
}

func (ck *checker) checkMeta() {
	fmt.Println("● 元信息 GET /api/meta")
	b, code := ck.get("/api/meta")
	ck.ok("/api/meta 返回 200", code == 200)
	want := `"poseOrder":["identity","rot90","rot180","rot270","mirrorH","mirrorHrot90","mirrorHrot180","mirrorHrot270"]`
	ck.ok("姿态顺序为固定规范顺序", bytes.Contains(b, []byte(want)), string(b))
}

// ---- 工具：生成二值方阵文本 ----

func matrixText(m []byte, n int) string {
	var buf bytes.Buffer
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if m[r*n+c] == 1 {
				buf.WriteByte('1')
			} else {
				buf.WriteByte('0')
			}
		}
		buf.WriteByte('\n')
	}
	return buf.String()
}

func randMatrix(rng *rand.Rand, n int, density float64) []byte {
	m := make([]byte, n*n)
	for i := range m {
		if rng.Float64() < density {
			m[i] = 1
		}
	}
	return m
}

func ones(m []byte) int {
	c := 0
	for _, v := range m {
		c += int(v)
	}
	return c
}

// insideCount 独立统计姿态 p 平移 (dy,dx) 后落在画布内的复检点数。
func insideCount(chk []byte, n, p, dy, dx int) int {
	bp := make([]byte, n*n)
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			nr, nc := xform(p, r, c, n)
			bp[nr*n+nc] = chk[r*n+c]
		}
	}
	in := 0
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if bp[r*n+c] == 0 {
				continue
			}
			tr, tc := r+dy, c+dx
			if tr >= 0 && tr < n && tc >= 0 && tc < n {
				in++
			}
		}
	}
	return in
}

func (ck *checker) crossCheckCase(name string, ref, chk []byte, n int) {
	res, e, code := ck.audit(matrixText(ref, n), matrixText(chk, n))
	if code != 200 {
		ck.ok(name+" API 200", false, fmt.Sprintf("code=%d err=%v", code, e))
		return
	}
	sol := brute(ref, chk, n)
	ov := bruteOverlay(ref, chk, n, sol.canonical)

	ck.ok(name+" 最大重合与暴力一致", res.MaxOverlap == sol.max,
		fmt.Sprintf("api=%d brute=%d", res.MaxOverlap, sol.max))
	ck.ok(name+" 并列最优数与暴力一致", res.TieCount == sol.tie,
		fmt.Sprintf("api=%d brute=%d", res.TieCount, sol.tie))
	ck.ok(name+" 规范解姿态/纵移/横移一致",
		res.Canonical.PoseIndex == sol.canonical.pose &&
			res.Canonical.DY == sol.canonical.dy && res.Canonical.DX == sol.canonical.dx,
		fmt.Sprintf("api=(%s,%d,%d) brute=(%d,%d,%d)",
			res.Canonical.Pose, res.Canonical.DY, res.Canonical.DX,
			sol.canonical.pose, sol.canonical.dy, sol.canonical.dx))
	ck.ok(name+" 原图缺陷总数（含移出画布）",
		res.ReferenceTotal == ones(ref) && res.RecheckTotal == ones(chk),
		fmt.Sprintf("api=(%d,%d) real=(%d,%d)", res.ReferenceTotal, res.RecheckTotal, ones(ref), ones(chk)))
	wantOutside := ones(chk) - insideCount(chk, n, sol.canonical.pose, sol.canonical.dy, sol.canonical.dx)
	ck.ok(name+" 移出画布缺陷数与暴力一致", res.OutsideCanvas == wantOutside,
		fmt.Sprintf("api=%d brute=%d", res.OutsideCanvas, wantOutside))
	overlayEqual := len(res.Overlay) == len(ov)
	if overlayEqual {
		for i := range ov {
			if res.Overlay[i] != ov[i] {
				overlayEqual = false
				break
			}
		}
	}
	ck.ok(name+" 红蓝叠加逐格与暴力重建一致", overlayEqual)
	ck.ok(name+" 遍历空间为 8×(2n-1)^2",
		res.SearchSpace.Poses == 8 &&
			res.SearchSpace.ShiftsPerPose == (2*n-1)*(2*n-1) &&
			res.SearchSpace.TotalTriples == 8*(2*n-1)*(2*n-1))
}

func (ck *checker) checkRandomCrossCheck() {
	fmt.Println("● 随机点阵：真实 API 结果与独立暴力遍历交叉核对")
	rng := rand.New(rand.NewSource(20260920))
	cases := []struct {
		n       int
		density float64
	}{
		{16, 0.1}, {16, 0.5}, {24, 0.03}, {32, 0.15}, {40, 0.4}, {48, 0.08},
	}
	for i, tc := range cases {
		ref := randMatrix(rng, tc.n, tc.density)
		chk := randMatrix(rng, tc.n, tc.density)
		ck.crossCheckCase(fmt.Sprintf("用例%d(n=%d,密度%.2f)", i+1, tc.n, tc.density), ref, chk, tc.n)
	}
}

func (ck *checker) checkKnownTransform() {
	fmt.Println("● 已知变换还原：rot180 + 平移 (3,-5)，规范解必须唯一还原")
	n := 48
	rng := rand.New(rand.NewSource(99))
	ref := make([]byte, n*n)
	chk := make([]byte, n*n)
	for k := 0; k < 60; k++ {
		r, c := rng.Intn(n), rng.Intn(n)
		if ref[r*n+c] == 1 {
			continue
		}
		ref[r*n+c] = 1
		qr, qc := n-1-(r-3), n-1-(c+5)
		if qr >= 0 && qr < n && qc >= 0 && qc < n {
			chk[qr*n+qc] = 1
		}
	}
	res, e, code := ck.audit(matrixText(ref, n), matrixText(chk, n))
	ck.ok("API 200", code == 200, fmt.Sprintf("%v", e))
	if code == 200 {
		ck.ok("规范姿态为 rot180(索引2)", res.Canonical.PoseIndex == 2,
			fmt.Sprintf("got %d(%s)", res.Canonical.PoseIndex, res.Canonical.Pose))
		ck.ok("规范平移为 (dy=3, dx=-5)",
			res.Canonical.DY == 3 && res.Canonical.DX == -5,
			fmt.Sprintf("got (%d,%d)", res.Canonical.DY, res.Canonical.DX))
		ck.ok("最大重合等于落回画布的构造点数",
			res.MaxOverlap == ones(chk), fmt.Sprintf("overlap=%d chk=%d", res.MaxOverlap, ones(chk)))
	}
}

func (ck *checker) checkOutsideCanvas() {
	fmt.Println("● 移出画布：复检点平移后越界仍计入原图总数")
	n := 20
	ref := make([]byte, n*n)
	chk := make([]byte, n*n)
	// 参考图在 (0,0)、复检图在 (n-1,n-1)：仅验证原图总数与越界点计数语义。
	ref[0] = 1
	chk[n*n-1] = 1
	res, e, code := ck.audit(matrixText(ref, n), matrixText(chk, n))
	ck.ok("API 200", code == 200, fmt.Sprintf("%v", e))
	if code == 200 {
		ck.ok("复检图总数仍为 1（移出者计入）", res.RecheckTotal == 1)
	}

	ref2 := make([]byte, n*n)
	// 点放在 (0,0)：规范解 identity/dy=dx=-19 把它移到 (-19,-19)，移出画布。
	chk2 := make([]byte, n*n)
	chk2[0] = 1
	res2, e2, code2 := ck.audit(matrixText(ref2, n), matrixText(chk2, n))
	ck.ok("空参考图 API 200", code2 == 200, fmt.Sprintf("%v", e2))
	if code2 == 200 {
		ck.ok("全空间并列 tie=8*(2n-1)^2",
			res2.TieCount == 8*(2*n-1)*(2*n-1), fmt.Sprintf("got %d", res2.TieCount))
		ck.ok("规范解 identity/dy=-19/dx=-19，该点移出画布",
			res2.Canonical.PoseIndex == 0 && res2.Canonical.DY == -19 && res2.Canonical.DX == -19,
			fmt.Sprintf("got (%s,%d,%d)", res2.Canonical.Pose, res2.Canonical.DY, res2.Canonical.DX))
		ck.ok("移出画布数=1 而原图总数仍=1",
			res2.OutsideCanvas == 1 && res2.RecheckTotal == 1,
			fmt.Sprintf("outside=%d total=%d", res2.OutsideCanvas, res2.RecheckTotal))
	}
}

func (ck *checker) checkDense512() {
	fmt.Println("● 稠密满尺寸 512×512：非逐点尝试平移，快速返回且数值正确")
	n := 512
	a := make([]byte, n*n)
	b := make([]byte, n*n)
	for i := range a {
		a[i] = 1
		b[i] = 1
	}
	t0 := time.Now()
	res, e, code := ck.audit(matrixText(a, n), matrixText(b, n))
	elapsed := time.Since(t0)
	ck.ok("API 200", code == 200, fmt.Sprintf("%v", e))
	if code == 200 {
		ck.ok("最大重合 = 512*512", res.MaxOverlap == n*n, fmt.Sprintf("got %d", res.MaxOverlap))
		ck.ok("全 1 矩阵八姿态在 (0,0) 并列，tie=8", res.TieCount == 8,
			fmt.Sprintf("got %d", res.TieCount))
		ck.ok("3MiB 稠密输入 10 秒内完成（O(n^2 log n) 频域相关）",
			elapsed < 10*time.Second, fmt.Sprintf("elapsed=%v", elapsed))
	}
}

func (ck *checker) checkValidation() {
	fmt.Println("● 非法输入：尺寸/字符/行宽错误定位到具体输入")
	good := matrixText(make([]byte, 16*16), 16)

	// 1) 复检图含非法字符，定位字段、行、列
	badRows := make([]string, 16)
	for i := range badRows {
		b := make([]byte, 16)
		for j := range b {
			b[j] = '0'
		}
		badRows[i] = string(b)
	}
	tmp := []byte(badRows[3])
	tmp[7] = '2'
	badRows[3] = string(tmp)
	var illegal bytes.Buffer
	for _, r := range badRows {
		illegal.WriteString(r + "\n")
	}
	_, e, code := ck.audit(good, illegal.String())
	ck.ok("非法字符返回 422", code == 422, fmt.Sprintf("code=%d", code))
	if e != nil && len(e.Errors) >= 1 {
		fe := e.Errors[0]
		ck.ok("定位到复检图/illegal_char/第4行第8列",
			fe.Field == "recheck" && fe.Code == "illegal_char" && fe.Line == 4 && fe.Column == 8,
			fmt.Sprintf("%+v", fe))
	}

	// 2) 参考图行宽不一致（使用全新干净的行，避免与非法字符用例串数据）
	cleanRows := make([]string, 16)
	for i := range cleanRows {
		b := make([]byte, 16)
		for j := range b {
			b[j] = '0'
		}
		cleanRows[i] = string(b)
	}
	cleanRows[5] = cleanRows[5][:10]
	var ragged bytes.Buffer
	for _, r := range cleanRows {
		ragged.WriteString(r + "\n")
	}
	_, e2, code2 := ck.audit(ragged.String(), good)
	loc := false
	if e2 != nil {
		for _, fe := range e2.Errors {
			if fe.Field == "reference" && fe.Code == "ragged_width" && fe.Line == 6 {
				loc = true
			}
		}
	}
	ck.ok("行宽错误定位到参考图第 6 行", code2 == 422 && loc, fmt.Sprintf("code=%d %+v", code2, e2))

	// 3) 边长越界（4×4）
	small := "0101\n1010\n0101\n1010\n"
	_, e3, code3 := ck.audit(small, good)
	loc3 := e3 != nil && len(e3.Errors) > 0 && e3.Errors[0].Field == "reference" &&
		e3.Errors[0].Code == "side_out_of_range"
	ck.ok("边长 4 定位到参考图 side_out_of_range", code3 == 422 && loc3, fmt.Sprintf("%+v", e3))

	// 4) 非方阵：17 行 16 列
	var nonsq bytes.Buffer
	for i := 0; i < 17; i++ {
		nonsq.WriteString("0000000000000000\n")
	}
	_, e4, code4 := ck.audit(nonsq.String(), good)
	loc4 := e4 != nil && len(e4.Errors) > 0 && e4.Errors[0].Field == "reference" &&
		e4.Errors[0].Code == "not_square"
	ck.ok("非方阵定位到参考图 not_square", code4 == 422 && loc4, fmt.Sprintf("%+v", e4))

	// 5) 两图边长不一致（参考图 16，复检图 32），定位复检图
	good32 := matrixText(make([]byte, 32*32), 32)
	_, e5, code5 := ck.audit(good, good32)
	loc5 := false
	if e5 != nil {
		for _, fe := range e5.Errors {
			if fe.Field == "recheck" && fe.Code == "size_mismatch" {
				loc5 = true
			}
		}
	}
	ck.ok("边长不一致定位到复检图 size_mismatch", code5 == 422 && loc5, fmt.Sprintf("%+v", e5))

	// 6) 两处错误同时报告
	_, e6, code6 := ck.audit(small, illegal.String())
	both := false
	if e6 != nil {
		seen := map[string]bool{}
		for _, fe := range e6.Errors {
			seen[fe.Field] = true
		}
		both = seen["reference"] && seen["recheck"]
	}
	ck.ok("两处同时非法时两个输入都被报告", code6 == 422 && both, fmt.Sprintf("%+v", e6))
}

func (ck *checker) checkRetryAfterFix() {
	fmt.Println("● 修正后原样重试：非法请求不留旧结论，随后同路径合法请求成功")
	good := matrixText(make([]byte, 16*16), 16)

	// 先发一次合法请求
	ok1, e1, c1 := ck.audit(good, good)
	ck.ok("首次合法请求成功", c1 == 200, fmt.Sprintf("%v", e1))
	// 再发非法请求，必须失败
	_, e2, c2 := ck.audit("not a matrix", good)
	ck.ok("随后非法请求返回 422", c2 == 422, fmt.Sprintf("code=%d %v", c2, e2))
	// 最后再次发合法请求，服务不得残留/串结论
	ok3, e3, c3 := ck.audit(good, good)
	cond := c3 == 200 && ok3 != nil && ok3.MaxOverlap == 0 && ok3.N == 16 && ok1.MaxOverlap == 0
	ck.ok("修正后原样重试成功且结论正确", cond, fmt.Sprintf("code=%d %v", c3, e3))
}

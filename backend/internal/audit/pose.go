package audit

// pose.go —— 正方形二值点阵的八种规范姿态（旋转/镜像）
//
// 约定：对复检图 B（边长 n）施加姿态变换得到 B'，随后把 B' 平移 (dy,dx)
// （正 dy 向下、正 dx 向右），统计与参考图 A 的重合缺陷数。
//
// 姿态固定顺序（也是规范解的第一裁决键）：
//   0 identity        坐标 (r, c) -> (r, c)
//   1 rot90           顺时针 90°   (r, c) -> (c, n-1-r)
//   2 rot180          180°         (r, c) -> (n-1-r, n-1-c)
//   3 rot270          顺时针 270°  (r, c) -> (n-1-c, r)
//   4 mirrorH         水平镜像（上下翻转）(r, c) -> (n-1-r, c)
//   5 mirrorHrot90    水平镜像后顺时针 90°
//   6 mirrorHrot180   水平镜像后 180°（等价于垂直镜像/左右翻转）
//   7 mirrorHrot270   水平镜像后顺时针 270°

const PoseCount = 8

// PoseName 固定姿态名称（顺序即裁决顺序）。
var PoseName = [PoseCount]string{
	"identity", "rot90", "rot180", "rot270",
	"mirrorH", "mirrorHrot90", "mirrorHrot180", "mirrorHrot270",
}

// transformPoint 给出姿态 p 下 B 内坐标 (r,c) 变到 B' 内的坐标 (r',c')。
//
// 由 rot90: (r,c)->(c,n-1-r)、mirrorH: (r,c)->(n-1-r,c) 复合得到全部八种。
func transformPoint(p, r, c, n int) (int, int) {
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
	case 5: // mirrorH 后 rot90：先 (n-1-r,c)，再 (c, r)
		return c, r
	case 6: // mirrorH 后 rot180：(r, n-1-c)
		return r, n - 1 - c
	case 7: // mirrorH 后 rot270：先 (n-1-r,c)，再 (n-1-c, n-1-r)
		return n - 1 - c, n - 1 - r
	default:
		panic("pose out of range")
	}
}

// applyPose 返回 B 经姿态 p 变换后的点阵（新分配的边长 n 的扁平切片，行主序）。
func applyPose(p int, b []byte, n int) []byte {
	out := make([]byte, n*n)
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			nr, nc := transformPoint(p, r, c, n)
			out[nr*n+nc] = b[r*n+c]
		}
	}
	return out
}

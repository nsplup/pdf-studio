package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// NaturalLess 自然序比较："part_2" < "part_10"。
// 按 数字/非数字 分段逐段比较，数字段按数值比较。
func NaturalLess(a, b string) bool {
	return compareNatural(a, b) < 0
}

// NaturalSortStrings 对文件路径按文件名自然序原地排序。
func NaturalSortStrings(paths []string) {
	// 简单插入排序即可：输入规模通常为个位数到几十个文件
	for i := 1; i < len(paths); i++ {
		for j := i; j > 0 && compareNatural(filepath.Base(paths[j]), filepath.Base(paths[j-1])) < 0; j-- {
			paths[j], paths[j-1] = paths[j-1], paths[j]
		}
	}
}

func compareNatural(a, b string) int {
	ai, bi := 0, 0
	for ai < len(a) && bi < len(b) {
		ca, cb := a[ai], b[bi]
		aDigit, bDigit := isDigit(ca), isDigit(cb)
		switch {
		case aDigit && bDigit:
			// 提取两段完整数字
			aj := ai
			for aj < len(a) && isDigit(a[aj]) {
				aj++
			}
			bj := bi
			for bj < len(b) && isDigit(b[bj]) {
				bj++
			}
			na := trimLeadingZeros(a[ai:aj])
			nb := trimLeadingZeros(b[bi:bj])
			if len(na) != len(nb) {
				if len(na) < len(nb) {
					return -1
				}
				return 1
			}
			if c := strings.Compare(na, nb); c != 0 {
				return c
			}
			// 数值相等时，前导零少者靠前（稳定）
			if la, lb := aj-ai, bj-bi; la != lb {
				if la < lb {
					return -1
				}
				return 1
			}
			ai, bi = aj, bj
		case aDigit != bDigit:
			// 数字段与非数字段：数字按字符比较即可满足常见预期（'0'<'a'）
			if ca != cb {
				if ca < cb {
					return -1
				}
				return 1
			}
			ai++
			bi++
		default:
			if ca != cb {
				if ca < cb {
					return -1
				}
				return 1
			}
			ai++
			bi++
		}
	}
	switch {
	case ai < len(a):
		return 1
	case bi < len(b):
		return -1
	}
	return 0
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func trimLeadingZeros(s string) string {
	i := 0
	for i < len(s)-1 && s[i] == '0' {
		i++
	}
	return s[i:]
}

var _ = fmt.Sprintf
var _ = os.Getpid

// timeSleep 测试辅助。
func timeSleep(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }

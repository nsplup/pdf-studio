package engine

import (
	"strconv"
	"strings"
)

func splitComma(s string) []string {
	return strings.Split(s, ",")
}

func indexByte(s string, c byte) int {
	return strings.IndexByte(s, c)
}

func atoi(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, false
	}
	return n, true
}

// splitSelection 把 "1-3,5" 拆为 ["1-3","5"]（pdfcpu 每个 token 一个区间）。
func splitSelection(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

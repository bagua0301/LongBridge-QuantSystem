package utils

import (
	"strings"
)

// TablePrinter 表格打印机
type TablePrinter struct {
	headers        []string
	widths         []int
	rows           [][]string
	useSimpleChars bool
	result         strings.Builder
	alignLeft      bool
}

// NewTablePrinter 创建表格打印机
func NewTablePrinter(headers []string) *TablePrinter {
	return &TablePrinter{
		headers:        headers,
		useSimpleChars: false,
		rows:           make([][]string, 0),
		alignLeft:      true,
	}
}

// SetAlignLeft 设置左对齐模式
func (tp *TablePrinter) SetAlignLeft(alignLeft bool) {
	tp.alignLeft = alignLeft
}

// SetUseSimpleChars 设置是否使用简单字符
func (tp *TablePrinter) SetUseSimpleChars(useSimple bool) {
	tp.useSimpleChars = useSimple
}

// AddRow 添加数据行（自动将单个 % 转义为 %%）
func (tp *TablePrinter) AddRow(row []string) {
	processedRow := make([]string, len(row))
	for i, cell := range row {
		processedRow[i] = strings.ReplaceAll(cell, "%%", "%")
	}
	tp.rows = append(tp.rows, processedRow)
}

// isEmoji 判断是否是 emoji 字符
func isEmoji(r rune) bool {
	return (r >= 0x1F600 && r <= 0x1F64F) ||
		(r >= 0x1F300 && r <= 0x1F5FF) ||
		(r >= 0x1F680 && r <= 0x1F6FF) ||
		(r >= 0x2600 && r <= 0x26FF) ||
		(r >= 0x2700 && r <= 0x27BF) ||
		(r >= 0xFE00 && r <= 0xFE0F) ||
		(r >= 0x1F900 && r <= 0x1F9FF) ||
		(r >= 0x1FA00 && r <= 0x1FA6F)
}

// getCharWidth 获取单个字符的显示宽度
func getCharWidth(r rune) int {
	if isEmoji(r) {
		return 2
	}
	if r >= 0x4E00 && r <= 0x9FFF {
		return 2
	}
	if r >= 0xFF00 && r <= 0xFFEF {
		return 2
	}
	if r >= 0x3040 && r <= 0x309F {
		return 2
	}
	if r >= 0x30A0 && r <= 0x30FF {
		return 2
	}
	return 1
}

// getDisplayWidth
func getDisplayWidth(s string) int {
	width := 0
	for _, r := range s {
		width += getCharWidth(r)
	}
	return width
}

// roundToEven 向上取整到最近的偶数
func roundToEven(n int) int {
	if n%2 == 0 {
		return n
	}
	return n + 1
}

// calculateWidths 计算列宽
func (tp *TablePrinter) calculateWidths() {
	if len(tp.headers) == 0 {
		return
	}

	tp.widths = make([]int, len(tp.headers))
	for i, header := range tp.headers {
		tp.widths[i] = getDisplayWidth(header)
	}

	for _, row := range tp.rows {
		for i, cell := range row {
			if i < len(tp.widths) {
				cellWidth := getDisplayWidth(cell)
				if cellWidth > tp.widths[i] {
					tp.widths[i] = cellWidth
				}
			}
		}
	}

	for i := range tp.widths {
		tp.widths[i] = tp.widths[i] + 2
		tp.widths[i] = roundToEven(tp.widths[i])
	}
}

// getChars 获取表格字符
func (tp *TablePrinter) getChars() (topLeft, topMid, topRight, midLeft, midMid, midRight, bottomLeft, bottomMid, bottomRight, vertical, horizontal string) {
	if tp.useSimpleChars {
		topLeft = "+"
		topMid = "+"
		topRight = "+"
		midLeft = "+"
		midMid = "+"
		midRight = "+"
		bottomLeft = "+"
		bottomMid = "+"
		bottomRight = "+"
		vertical = "|"
		horizontal = "-"
	} else {
		topLeft = "┌"
		topMid = "┬"
		topRight = "┐"
		midLeft = "├"
		midMid = "┼"
		midRight = "┤"
		bottomLeft = "└"
		bottomMid = "┴"
		bottomRight = "┘"
		vertical = "│"
		horizontal = "─"
	}
	return
}

// buildBorder 构建边框
func (tp *TablePrinter) buildBorder(left, mid, right string) string {
	if len(tp.widths) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString(left)

	for i, w := range tp.widths {
		builder.WriteString(strings.Repeat(tp.getCharsHorizontal(), w))
		if i < len(tp.widths)-1 {
			builder.WriteString(mid)
		}
	}

	builder.WriteString(right)
	return builder.String()
}

// getCharsHorizontal 获取水平线字符
func (tp *TablePrinter) getCharsHorizontal() string {
	_, _, _, _, _, _, _, _, _, _, horizontal := tp.getChars()
	return horizontal
}

// getCharsVertical 获取垂直线字符
func (tp *TablePrinter) getCharsVertical() string {
	_, _, _, _, _, _, _, _, _, vertical, _ := tp.getChars()
	return vertical
}

// getDisplayString 获取用于显示的字符串（将 %% 保留，直接输出）
func getDisplayString(s string) string {
	return s
}

// padStringLeft 左对齐填充
func padStringLeft(s string, width int) string {
	currentWidth := getDisplayWidth(s)
	if currentWidth >= width {
		return s
	}
	padding := width - currentWidth
	return s + strings.Repeat(" ", padding)
}

// buildHeaderRow 构建表头行
func (tp *TablePrinter) buildHeaderRow() string {
	if len(tp.widths) == 0 {
		return ""
	}

	var builder strings.Builder
	vertical := tp.getCharsVertical()
	builder.WriteString(vertical)

	for i, w := range tp.widths {
		header := tp.headers[i]
		builder.WriteString(" ")
		builder.WriteString(padStringLeft(header, w-1))
		builder.WriteString(vertical)
	}

	return builder.String()
}

// buildDataRow 构建数据行
func (tp *TablePrinter) buildDataRow(row []string) string {
	if len(tp.widths) == 0 {
		return ""
	}

	var builder strings.Builder
	vertical := tp.getCharsVertical()
	builder.WriteString(vertical)

	for i, w := range tp.widths {
		cell := ""
		if i < len(row) {
			cell = row[i]
		}
		builder.WriteString(" ")
		builder.WriteString(padStringLeft(cell, w-1))
		builder.WriteString(vertical)
	}

	return builder.String()
}

// Build 构建整个表格
func (tp *TablePrinter) Build() string {
	tp.result.Reset()

	tp.calculateWidths()

	if len(tp.widths) == 0 {
		return ""
	}

	topLeft, topMid, topRight, midLeft, midMid, midRight, bottomLeft, bottomMid, bottomRight, _, _ := tp.getChars()

	tp.result.WriteString(tp.buildBorder(topLeft, topMid, topRight))
	tp.result.WriteString("\n")

	tp.result.WriteString(tp.buildHeaderRow())
	tp.result.WriteString("\n")

	tp.result.WriteString(tp.buildBorder(midLeft, midMid, midRight))
	tp.result.WriteString("\n")

	for i, row := range tp.rows {
		line := strings.ReplaceAll(tp.buildDataRow(row), "%", "%%")
		tp.result.WriteString(line)
		if i < len(tp.rows)-1 {
			tp.result.WriteString("\n")
		}
	}

	if len(tp.rows) > 0 {
		tp.result.WriteString("\n")
	}
	tp.result.WriteString(tp.buildBorder(bottomLeft, bottomMid, bottomRight))

	return tp.result.String()
}

// PrintToLogger 打印表格到 logger
func (tp *TablePrinter) PrintToLogger(loggerFunc func(string, ...interface{})) {
	tableStr := tp.Build()
	for _, line := range strings.Split(tableStr, "\n") {
		if line != "" {
			loggerFunc(line)
		}
	}
}

// Clear 清空表格数据
func (tp *TablePrinter) Clear() {
	tp.rows = make([][]string, 0)
	tp.widths = nil
	tp.result.Reset()
}

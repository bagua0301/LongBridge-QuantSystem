package utils

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/longportapp/openapi-go/quote"
	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"
)

// TradeStatus 常量值（根据 SDK 实际定义）
// 这些值需要根据 SDK 文档确认，以下是常见值
const (
	TradeStatusNormal    quote.TradeStatus = 0  // 正常交易
	TradeStatusHalted    quote.TradeStatus = 1  // 停牌
	TradeStatusDelisted  quote.TradeStatus = 2  // 退市
	TradeStatusSuspended quote.TradeStatus = 3  // 暂停交易
	TradeStatusPreOpen   quote.TradeStatus = 4  // 盘前
	TradeStatusPreClose  quote.TradeStatus = 5  // 盘后
	TradeStatusClosed    quote.TradeStatus = 6  // 收盘
	TradeStatusUnknown   quote.TradeStatus = 99 // 未知
)

// TradeSessionType 常量值
const (
	TradeSessionTypePreMarket  quote.TradeSessionType = 0  // 盘前
	TradeSessionTypeRegular    quote.TradeSessionType = 1  // 盘中
	TradeSessionTypePostMarket quote.TradeSessionType = 2  // 盘后
	TradeSessionTypeOvernight  quote.TradeSessionType = 3  // 夜盘
	TradeSessionTypeUnknown    quote.TradeSessionType = 99 // 未知
)

// TradeStatusMap 交易状态字符串到整数的映射
var TradeStatusMap = map[string]quote.TradeStatus{
	"Normal":    TradeStatusNormal,
	"Halted":    TradeStatusHalted,
	"Delisted":  TradeStatusDelisted,
	"Suspended": TradeStatusSuspended,
	"PreOpen":   TradeStatusPreOpen,
	"PreClose":  TradeStatusPreClose,
	"Closed":    TradeStatusClosed,
	"Unknown":   TradeStatusUnknown,
}

// TradeSessionMap 交易时段字符串到整数的映射
var TradeSessionMap = map[string]quote.TradeSessionType{
	"PreMarket":  TradeSessionTypePreMarket,
	"Regular":    TradeSessionTypeRegular,
	"PostMarket": TradeSessionTypePostMarket,
	"Overnight":  TradeSessionTypeOvernight,
	"Unknown":    TradeSessionTypeUnknown,
}

// ExcelQuoteReader Excel行情数据读取器
type ExcelQuoteReader struct {
	filePath string
}

// NewExcelQuoteReader 创建Excel读取器
func NewExcelQuoteReader(filePath string) *ExcelQuoteReader {
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(GetExecpath(), filePath)
	}
	return &ExcelQuoteReader{filePath: filePath}
}

// parseTradeStatus 解析交易状态字符串
func parseTradeStatus(statusStr string) quote.TradeStatus {
	value, err := StringToInt32(statusStr)
	if err != nil {
		return TradeStatusUnknown
	}
	return quote.TradeStatus(value)
}

// parseTradeSession 解析交易时段字符串
func parseTradeSession(sessionStr string) quote.TradeSessionType {
	value, err := StringToInt32(sessionStr)
	if err != nil {
		return TradeSessionTypeUnknown
	}
	return quote.TradeSessionType(value)
}

// ReadQuotes 读取Excel中的行情数据
func (r *ExcelQuoteReader) ReadQuotes(sheetName string) ([]*quote.PushQuote, error) {
	f, err := excelize.OpenFile(r.filePath)
	if err != nil {
		return nil, fmt.Errorf("打开Excel文件失败: %v", err)
	}
	defer f.Close()

	if sheetName == "" {
		sheetName = f.GetSheetName(0)
	}

	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("读取工作表失败: %v", err)
	}

	if len(rows) < 2 {
		return nil, fmt.Errorf("数据行数不足")
	}

	var quotes []*quote.PushQuote
	for i, row := range rows[1:] {
		if len(row) < 11 {
			continue
		}

		q, err := r.parseRow(row, i)
		if err != nil {
			fmt.Printf("解析第%d行失败: %v\n", i+2, err)
			continue
		}
		quotes = append(quotes, q)
	}

	return quotes, nil
}

// parseRow 解析单行数据
func (r *ExcelQuoteReader) parseRow(row []string, lineNum int) (*quote.PushQuote, error) {
	q := &quote.PushQuote{}

	// Symbol (列A)
	if len(row) > 0 && row[0] != "" {
		q.Symbol = row[0]
	} else {
		return nil, fmt.Errorf("Symbol不能为空")
	}

	// Sequence (列B)
	if len(row) > 1 && row[1] != "" {
		seqStr := strings.TrimSpace(row[1])
		var seq int64
		var err error

		if strings.Contains(seqStr, "E") || strings.Contains(seqStr, "e") {
			var f float64
			f, err = strconv.ParseFloat(seqStr, 64)
			if err == nil {
				seq = int64(f)
			}
		} else {
			seq, err = strconv.ParseInt(seqStr, 10, 64)
		}

		if err != nil {
			return nil, fmt.Errorf("Sequence解析失败: %v, 原始值=%s", err, seqStr)
		}
		q.Sequence = seq
	}

	// LastDone (列C)
	if len(row) > 2 && row[2] != "" {
		price, err := strconv.ParseFloat(row[2], 64)
		if err != nil {
			return nil, fmt.Errorf("LastDone解析失败: %v", err)
		}
		dec := decimal.NewFromFloat(price)
		q.LastDone = &dec
	}

	// Open (列D) - 可能为空
	if len(row) > 3 && row[3] != "" {
		price, err := strconv.ParseFloat(row[3], 64)
		if err == nil && price != 0 {
			dec := decimal.NewFromFloat(price)
			q.Open = &dec
		}
	}

	// High (列E) - 可能为空
	if len(row) > 4 && row[4] != "" {
		price, err := strconv.ParseFloat(row[4], 64)
		if err == nil && price != 0 {
			dec := decimal.NewFromFloat(price)
			q.High = &dec
		}
	}

	// Low (列F) - 可能为空
	if len(row) > 5 && row[5] != "" {
		price, err := strconv.ParseFloat(row[5], 64)
		if err == nil && price != 0 {
			dec := decimal.NewFromFloat(price)
			q.Low = &dec
		}
	}

	// Timestamp (列G)
	if len(row) > 6 && row[6] != "" {
		ts, err := strconv.ParseInt(row[6], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("Timestamp解析失败: %v", err)
		}
		q.Timestamp = ts
	}

	// Volume (列H)
	if len(row) > 7 && row[7] != "" {
		vol, err := strconv.ParseInt(row[7], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("Volume解析失败: %v", err)
		}
		q.Volume = vol
	}

	// Turnover (列I)
	if len(row) > 8 && row[8] != "" {
		turnover, err := strconv.ParseFloat(row[8], 64)
		if err != nil {
			return nil, fmt.Errorf("Turnover解析失败: %v", err)
		}
		dec := decimal.NewFromFloat(turnover)
		q.Turnover = &dec
	}

	// TradeStatus (列J)
	if len(row) > 9 && row[9] != "" {
		q.TradeStatus = parseTradeStatus(row[9])
	} else {
		// 默认为未知(测试专用)
		q.TradeStatus = TradeStatusUnknown
	}

	// TradeSession (列K)
	if len(row) > 10 && row[10] != "" {
		q.TradeSession = parseTradeSession(row[10])
	} else {
		// 默认为未知(测试专用)
		q.TradeSession = TradeSessionTypeUnknown
	}

	return q, nil
}

// ReadQuotesFromMemory 从内存数据读取（用于测试）
func (r *ExcelQuoteReader) ReadQuotesFromMemory(data [][]interface{}) ([]*quote.PushQuote, error) {
	var quotes []*quote.PushQuote
	for i, row := range data {
		if len(row) < 11 {
			continue
		}
		q, err := r.parseRowFromInterface(row, i+1)
		if err != nil {
			continue
		}
		quotes = append(quotes, q)
	}
	return quotes, nil
}

// parseRowFromInterface 从interface切片解析
func (r *ExcelQuoteReader) parseRowFromInterface(row []interface{}, lineNum int) (*quote.PushQuote, error) {
	q := &quote.PushQuote{}

	getString := func(val interface{}) string {
		if val == nil {
			return ""
		}
		return fmt.Sprintf("%v", val)
	}

	getFloat64 := func(val interface{}) float64 {
		if val == nil {
			return 0
		}
		switch v := val.(type) {
		case float64:
			return v
		case int:
			return float64(v)
		case int64:
			return float64(v)
		case string:
			f, _ := strconv.ParseFloat(v, 64)
			return f
		default:
			return 0
		}
	}

	getInt64 := func(val interface{}) int64 {
		if val == nil {
			return 0
		}
		switch v := val.(type) {
		case int64:
			return v
		case float64:
			return int64(v)
		case int:
			return int64(v)
		case string:
			i, _ := strconv.ParseInt(v, 10, 64)
			return i
		default:
			return 0
		}
	}

	// Symbol
	if len(row) > 0 && row[0] != nil {
		q.Symbol = getString(row[0])
	}

	// Sequence
	if len(row) > 1 && row[1] != nil {
		q.Sequence = getInt64(row[1])
	}

	// LastDone
	if len(row) > 2 && row[2] != nil {
		price := getFloat64(row[2])
		if price != 0 {
			dec := decimal.NewFromFloat(price)
			q.LastDone = &dec
		}
	}

	// Open
	if len(row) > 3 && row[3] != nil {
		price := getFloat64(row[3])
		if price != 0 {
			dec := decimal.NewFromFloat(price)
			q.Open = &dec
		}
	}

	// High
	if len(row) > 4 && row[4] != nil {
		price := getFloat64(row[4])
		if price != 0 {
			dec := decimal.NewFromFloat(price)
			q.High = &dec
		}
	}

	// Low
	if len(row) > 5 && row[5] != nil {
		price := getFloat64(row[5])
		if price != 0 {
			dec := decimal.NewFromFloat(price)
			q.Low = &dec
		}
	}

	// Timestamp
	if len(row) > 6 && row[6] != nil {
		q.Timestamp = getInt64(row[6])
	}

	// Volume
	if len(row) > 7 && row[7] != nil {
		q.Volume = getInt64(row[7])
	}

	// Turnover
	if len(row) > 8 && row[8] != nil {
		turnover := getFloat64(row[8])
		if turnover != 0 {
			dec := decimal.NewFromFloat(turnover)
			q.Turnover = &dec
		}
	}

	// TradeStatus
	if len(row) > 9 && row[9] != nil {
		statusStr := getString(row[9])
		if statusStr != "" {
			q.TradeStatus = parseTradeStatus(statusStr)
		}
	}

	// TradeSession
	if len(row) > 10 && row[10] != nil {
		sessionStr := getString(row[10])
		if sessionStr != "" {
			q.TradeSession = parseTradeSession(sessionStr)
		}
	}

	return q, nil
}

// GetSheetNames 获取所有工作表名称
func (r *ExcelQuoteReader) GetSheetNames() ([]string, error) {
	f, err := excelize.OpenFile(r.filePath)
	if err != nil {
		return nil, fmt.Errorf("打开Excel文件失败: %v", err)
	}
	defer f.Close()
	return f.GetSheetList(), nil
}

// ReadAllSheets 读取所有工作表
func (r *ExcelQuoteReader) ReadAllSheets() (map[string][]*quote.PushQuote, error) {
	f, err := excelize.OpenFile(r.filePath)
	if err != nil {
		return nil, fmt.Errorf("打开Excel文件失败: %v", err)
	}
	defer f.Close()

	result := make(map[string][]*quote.PushQuote)
	sheets := f.GetSheetList()

	for _, sheet := range sheets {
		quotes, err := r.ReadQuotes(sheet)
		if err != nil {
			return nil, fmt.Errorf("读取工作表 %s 失败: %v", sheet, err)
		}
		result[sheet] = quotes
	}

	return result, nil
}

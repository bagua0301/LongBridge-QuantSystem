package trading

import (
	"LongBridge-QuantSystem/utils"
	"sync"
	"time"
)

type TradingSession int

const (
	SessionRegular   TradingSession = 1 << 0 // 盘中 09:30-16:00
	SessionAfter     TradingSession = 1 << 1 // 盘后 16:00-20:00
	SessionPre       TradingSession = 1 << 2 // 盘前 04:00-09:30
	SessionOvernight TradingSession = 1 << 3 // 夜盘 20:00-次日04:00
)

type TradingTimeControl struct {
	allowedSessions int
	location        *time.Location
	holidays        map[string]bool // 全天休市日
	earlyCloseDays  map[string]bool // 提前收盘日（13:00 ET）
	year            int             // 当前年份（用于日志）
}

// HolidayRule 节假日规则（固定日期或浮动日期）
type HolidayRule struct {
	Name         string
	Month        time.Month
	Day          int          // 固定日期用
	Weekday      time.Weekday // 浮动日期用（如第三周的周一）
	Week         int          // 第几周，1-4或-1表示倒数第几周
	IsFloating   bool
	IsEarlyClose bool
}

// 全年10个全天休市日 + 2个提前收盘日的规则
var holidayRules = []HolidayRule{
	// 固定日期
	{Name: "New Year's Day", Month: time.January, Day: 1, IsFloating: false},
	{Name: "Juneteenth", Month: time.June, Day: 19, IsFloating: false},
	{Name: "Independence Day", Month: time.July, Day: 4, IsFloating: false},
	{Name: "Veterans Day", Month: time.November, Day: 11, IsFloating: false},
	{Name: "Christmas Day", Month: time.December, Day: 25, IsFloating: false},

	// 浮动日期（规则：某月的第几个周几）
	{Name: "Martin Luther King Jr. Day", Month: time.January, Weekday: time.Monday, Week: 3, IsFloating: true},
	{Name: "Presidents' Day", Month: time.February, Weekday: time.Monday, Week: 3, IsFloating: true},
	{Name: "Good Friday", Month: time.April, Weekday: time.Friday, Week: -1, IsFloating: true}, // 4月的最后一个周五
	{Name: "Memorial Day", Month: time.May, Weekday: time.Monday, Week: -1, IsFloating: true},
	{Name: "Labor Day", Month: time.September, Weekday: time.Monday, Week: 1, IsFloating: true},
	{Name: "Thanksgiving Day", Month: time.November, Weekday: time.Thursday, Week: 4, IsFloating: true},

	// 提前收盘日（仅用于记录，IsHoliday返回false）
	{Name: "Black Friday", Month: time.November, Weekday: time.Friday, Week: -1, IsFloating: true, IsEarlyClose: true},
	{Name: "Christmas Eve", Month: time.December, Day: 24, IsFloating: false, IsEarlyClose: true},
}

var (
	GlobalTrading *TradingTimeControl
	once          sync.Once
)

func CreateTradingTimeControl(nyLocation *time.Location, allowedMask int) *TradingTimeControl {
	once.Do(func() {
		GlobalTrading = NewTradingTimeControl(nyLocation, allowedMask)
	})
	return GlobalTrading
}

func GetTradingTimeControl() *TradingTimeControl {
	return GlobalTrading
}

// NewTradingTimeControl 创建交易时间控制器，自动加载当年及下一年的节假日和提前收盘日
func NewTradingTimeControl(nyLocation *time.Location, allowedMask int) *TradingTimeControl {
	now := utils.Now()
	currentYear := now.Year()
	tc := &TradingTimeControl{
		allowedSessions: allowedMask,
		location:        nyLocation,
		holidays:        make(map[string]bool),
		earlyCloseDays:  make(map[string]bool),
		year:            currentYear,
	}
	// 加载当前年份和下一年的数据，避免跨年问题
	tc.loadHolidaysForYears(currentYear, currentYear+1)
	return tc
}

// loadHolidaysForYears 为指定年份范围加载全天休市日和提前收盘日
func (tc *TradingTimeControl) loadHolidaysForYears(startYear, endYear int) {
	for year := startYear; year <= endYear; year++ {
		tc.loadHolidaysForYear(year)
		tc.loadEarlyCloseDaysForYear(year)
	}
}

// loadEarlyCloseDaysForYear 加载某年的提前收盘日
func (tc *TradingTimeControl) loadEarlyCloseDaysForYear(year int) {
	// 1. 黑色星期五：感恩节后一天
	thanksgiving := getFloatingHoliday(year, HolidayRule{
		Month:      time.November,
		Weekday:    time.Thursday,
		Week:       4,
		IsFloating: true,
	}, tc.location)
	if !thanksgiving.IsZero() {
		blackFriday := thanksgiving.AddDate(0, 0, 1)
		tc.earlyCloseDays[blackFriday.Format("2006-01-02")] = true
	}

	// 2. 平安夜：12月24日（固定日期）
	christmasEve := time.Date(year, time.December, 24, 0, 0, 0, 0, tc.location)
	tc.earlyCloseDays[christmasEve.Format("2006-01-02")] = true
}

// IsEarlyCloseDay 判断是否为提前收盘日
func (tc *TradingTimeControl) IsEarlyCloseDay(nyTime time.Time) bool {
	if tc.earlyCloseDays == nil {
		return false
	}
	dateKey := nyTime.Format("2006-01-02")
	return tc.earlyCloseDays[dateKey]
}

// loadHolidaysForYear 为指定年份生成节假日列表
func (tc *TradingTimeControl) loadHolidaysForYear(year int) {
	nyLoc := tc.location

	for _, rule := range holidayRules {
		// 跳过提前收盘日（这些日子市场是开市的，只提前收盘）
		if rule.IsEarlyClose {
			continue
		}
		var date time.Time
		if rule.IsFloating {
			date = getFloatingHoliday(year, rule, nyLoc)
		} else {
			date = getFixedHoliday(year, rule, nyLoc)
		}
		if !date.IsZero() {
			tc.holidays[date.Format("2006-01-02")] = true
		}
	}
}

// getFixedHoliday 处理固定日期节假日，自动处理调休规则
func getFixedHoliday(year int, rule HolidayRule, loc *time.Location) time.Time {
	target := time.Date(year, rule.Month, rule.Day, 0, 0, 0, 0, loc)
	weekday := target.Weekday()

	// 调休规则
	switch rule.Name {
	case "New Year's Day":
		// 元旦若逢周六不补休，逢周日则在周一补休
		if weekday == time.Sunday {
			return target.AddDate(0, 0, 1)
		}
		return target
	case "Independence Day", "Christmas Day":
		// 独立日、圣诞节：逢周六提前到周五休市，逢周日顺延至周一休市
		if weekday == time.Saturday {
			return target.AddDate(0, 0, -1)
		}
		if weekday == time.Sunday {
			return target.AddDate(0, 0, 1)
		}
		return target
	default:
		// 其余固定日期节日，直接返回原日期
		return target
	}
}

// getFloatingHoliday 计算浮动日期节假日
func getFloatingHoliday(year int, rule HolidayRule, loc *time.Location) time.Time {
	firstOfMonth := time.Date(year, rule.Month, 1, 0, 0, 0, 0, loc)
	firstWeekday := int(firstOfMonth.Weekday())
	targetWeekday := int(rule.Weekday)

	// 计算偏移量
	var offset int
	if rule.Week > 0 {
		// 正数：第N个周几
		offset = (targetWeekday - firstWeekday + 7) % 7
		offset += (rule.Week - 1) * 7
	} else {
		// 负数：倒数第N个周几（-1表示最后一个）
		daysInMonth := time.Date(year, rule.Month+1, 0, 0, 0, 0, 0, loc).Day()
		lastOfMonth := time.Date(year, rule.Month, daysInMonth, 0, 0, 0, 0, loc)
		lastWeekday := int(lastOfMonth.Weekday())
		// 从当月最后一天往前推算目标周几
		offset = (lastWeekday - targetWeekday + 7) % 7
		if offset > 0 {
			offset = 7 - offset
		}
		// 倒数第 rule.Week 个（如 -1 取最后一个，-2 取倒数第二个）
		weekOffset := (utils.Abs(rule.Week) - 1) * 7
		offset += weekOffset
		offset = -offset
	}

	return firstOfMonth.AddDate(0, 0, offset)
}

// SetHolidays 设置节假日列表（美东日期）
// dates: 字符串切片，格式 "2006-01-02"
func (tc *TradingTimeControl) SetHolidays(dates []string) {
	tc.holidays = make(map[string]bool)
	for _, d := range dates {
		tc.holidays[d] = true
	}
}

// AddHoliday 添加单个节假日
func (tc *TradingTimeControl) AddHoliday(date string) {
	if tc.holidays == nil {
		tc.holidays = make(map[string]bool)
	}
	tc.holidays[date] = true
}

// AddCustomHoliday 添加自定义节假日（覆盖自动生成的节假日）
func (tc *TradingTimeControl) AddCustomHoliday(date string) {
	tc.AddHoliday(date)
}

// RemoveHoliday 移除节假日（用于覆盖自动生成的节假日）
func (tc *TradingTimeControl) RemoveHoliday(date string) {
	if tc.holidays != nil {
		delete(tc.holidays, date)
	}
}

// IsHoliday 判断给定的美东时间是否为节假日
func (tc *TradingTimeControl) IsHoliday(nyTime time.Time) bool {
	if tc.holidays == nil {
		return false
	}
	dateKey := nyTime.Format("2006-01-02")
	return tc.holidays[dateKey]
}

// GetCurrentSession 获取当前交易时段（基于美东时间，支持提前收盘日）
func (tc *TradingTimeControl) GetCurrentSession() TradingSession {
	nyTime := utils.Now().In(tc.location)
	weekday := nyTime.Weekday()
	hour := nyTime.Hour()
	minute := nyTime.Minute()

	// 周末不交易
	if weekday == time.Saturday || weekday == time.Sunday {
		return 0
	}
	// 节假日不交易（所有时段均不可交易）
	if tc.IsHoliday(nyTime) {
		return 0
	}

	currentTime := hour*100 + minute

	// 提前收盘日：盘中结束时间变为 13:00
	regularEnd := 1600
	if tc.IsEarlyCloseDay(nyTime) {
		regularEnd = 1300
	}

	// 夜盘: 20:00 - 次日 04:00（跨日处理）
	if currentTime >= 2000 || currentTime < 400 {
		return SessionOvernight
	}
	// 盘前: 04:00 - 09:30
	if currentTime >= 400 && currentTime < 930 {
		return SessionPre
	}
	// 盘中: 09:30 - regularEnd
	if currentTime >= 930 && currentTime < regularEnd {
		return SessionRegular
	}
	// 盘后: regularEnd - 20:00（注意提前收盘日盘后时段从13:00开始）
	if currentTime >= regularEnd && currentTime < 2000 {
		return SessionAfter
	}

	return 0
}

// IsAllowed 检查当前时段是否允许交易
func (tc *TradingTimeControl) IsAllowed() bool {
	currentSession := tc.GetCurrentSession()
	if currentSession == 0 {
		return false
	}
	return (tc.allowedSessions & int(currentSession)) != 0
}

// GetAllowedSessionsDesc 获取允许时段的描述
func (tc *TradingTimeControl) GetAllowedSessionsDesc() string {
	desc := ""
	if tc.allowedSessions&int(SessionRegular) != 0 {
		desc += "盘中 "
	}
	if tc.allowedSessions&int(SessionAfter) != 0 {
		desc += "盘后 "
	}
	if tc.allowedSessions&int(SessionPre) != 0 {
		desc += "盘前 "
	}
	if tc.allowedSessions&int(SessionOvernight) != 0 {
		desc += "夜盘 "
	}
	if desc == "" {
		return "无"
	}
	return desc
}

// GetCurrentTimeInfo 获取当前时间信息（用于调试）
func (tc *TradingTimeControl) GetCurrentTimeInfo() map[string]interface{} {
	now := utils.Now()
	nyTime := now.In(tc.location)
	currentSession := tc.GetCurrentSession()

	sessionName := ""
	switch currentSession {
	case SessionRegular:
		sessionName = "盘中"
	case SessionAfter:
		sessionName = "盘后"
	case SessionPre:
		sessionName = "盘前"
	case SessionOvernight:
		sessionName = "夜盘"
	default:
		sessionName = "非交易时段"
	}

	return map[string]interface{}{
		"session":       sessionName,
		"session_value": currentSession,
		"is_allowed":    tc.IsAllowed(),
		"weekday":       nyTime.Weekday().String(),
		"hour_minute":   nyTime.Format("15:04"),
	}
}

// IsMarketOpen 判断市场是否开市（盘中时段）
func (tc *TradingTimeControl) IsMarketOpen() bool {
	return tc.GetCurrentSession() == SessionRegular
}

// GetNextSessionStart 获取下一个交易时段的开始时间
func (tc *TradingTimeControl) GetNextSessionStart() time.Time {
	nyTime := utils.Now().In(tc.location)
	currentSession := tc.GetCurrentSession()

	var nextStart time.Time

	switch currentSession {
	case SessionOvernight:
		// 当前是夜盘，下一个是盘前（今天 04:00）
		nextStart = time.Date(nyTime.Year(), nyTime.Month(), nyTime.Day(), 4, 0, 0, 0, tc.location)
		if nextStart.Before(nyTime) {
			nextStart = nextStart.Add(24 * time.Hour)
		}
	case SessionPre:
		// 当前是盘前，下一个是盘中（今天 09:30）
		nextStart = time.Date(nyTime.Year(), nyTime.Month(), nyTime.Day(), 9, 30, 0, 0, tc.location)
	case SessionRegular:
		// 当前是盘中，下一个是盘后（今天 16:00）
		nextStart = time.Date(nyTime.Year(), nyTime.Month(), nyTime.Day(), 16, 0, 0, 0, tc.location)
	case SessionAfter:
		// 当前是盘后，下一个是夜盘（今天 20:00）
		nextStart = time.Date(nyTime.Year(), nyTime.Month(), nyTime.Day(), 20, 0, 0, 0, tc.location)
	default:
		// 非交易时段，下一个是最近的交易时段开始
		nextStart = tc.findNextTradingStart(nyTime)
	}

	// 转换为北京时间返回
	return nextStart.In(time.Local)
}

// findNextTradingStart 查找下一个交易时段的开始时间
func (tc *TradingTimeControl) findNextTradingStart(nyTime time.Time) time.Time {
	// 从当前时间开始，按小时递增查找
	for i := 0; i < 48; i++ {
		checkTime := nyTime.Add(time.Duration(i) * time.Hour)
		// 转换为美东时间检查
		checkNY := checkTime.In(tc.location)
		hourMin := checkNY.Hour()*100 + checkNY.Minute()

		// 检查是否在任何一个交易时段开始点
		if hourMin == 400 || hourMin == 930 || hourMin == 1600 || hourMin == 2000 {
			if checkNY.Weekday() != time.Saturday && checkNY.Weekday() != time.Sunday && !tc.IsHoliday(checkNY) {
				return checkNY
			}
		}
	}
	return nyTime.Add(24 * time.Hour)
}

// GetSessionName 获取时段名称
func GetSessionName(session TradingSession) string {
	switch session {
	case SessionRegular:
		return "盘中"
	case SessionAfter:
		return "盘后"
	case SessionPre:
		return "盘前"
	case SessionOvernight:
		return "夜盘"
	default:
		return "非交易时段"
	}
}

// GetRegularSessionEnd 获取常规交易时段的收盘时间（美东）
// 若当天为提前收盘日，返回 13:00，否则返回 16:00
func (tc *TradingTimeControl) GetRegularSessionEnd(nyTime time.Time) time.Time {
	date := nyTime.In(tc.location)
	year, month, day := date.Year(), date.Month(), date.Day()
	if tc.IsEarlyCloseDay(date) {
		return time.Date(year, month, day, 13, 0, 0, 0, tc.location)
	}
	return time.Date(year, month, day, 16, 0, 0, 0, tc.location)
}

// ================= 通用前N分钟/最后N分钟判断 =================

// sessionTimeRange 获取指定交易时段的起止时间（美东时间，基于给定日期）
// 返回 (startTime, endTime, ok)
// 注意：夜盘跨日，startTime 和 endTime 可能跨过午夜
func (tc *TradingTimeControl) sessionTimeRange(date time.Time, session TradingSession) (start, end time.Time, ok bool) {
	nyDate := date.In(tc.location)
	year, month, day := nyDate.Year(), nyDate.Month(), nyDate.Day()

	switch session {
	case SessionRegular:
		start = time.Date(year, month, day, 9, 30, 0, 0, tc.location)
		end = tc.GetRegularSessionEnd(nyDate) // 动态收盘时间
		ok = true
	case SessionAfter:
		start = time.Date(year, month, day, 16, 0, 0, 0, tc.location)
		end = time.Date(year, month, day, 20, 0, 0, 0, tc.location)
		ok = true
	case SessionPre:
		start = time.Date(year, month, day, 4, 0, 0, 0, tc.location)
		end = time.Date(year, month, day, 9, 30, 0, 0, tc.location)
		ok = true
	case SessionOvernight:
		// 夜盘：当天 20:00 到次日 03:50
		start = time.Date(year, month, day, 20, 0, 0, 0, tc.location)
		end = time.Date(year, month, day, 3, 50, 0, 0, tc.location).Add(24 * time.Hour) // 次日 03:50
		ok = true
	default:
		ok = false
	}
	return
}

// IsInFirstNMinutesOfSessionAt 判断指定时间是否在某个交易时段开始后的前 N 分钟内
// t: 待判断的时间（本地时间，内部会转为美东）
// session: 交易时段（Regular/After/Pre/Overnight）
// n: 分钟数，必须大于0
// 若当天是节假日，该时段不存在，返回 false
func (tc *TradingTimeControl) IsInFirstNMinutesOfSessionAt(t time.Time, session TradingSession, n int) bool {
	if n <= 0 {
		return false
	}
	nyTime := t.In(tc.location)
	// 节假日所有时段均不可交易
	if tc.IsHoliday(nyTime) {
		return false
	}
	// 周末不交易
	weekday := nyTime.Weekday()
	if weekday == time.Saturday || weekday == time.Sunday {
		return false
	}

	start, end, ok := tc.sessionTimeRange(nyTime, session)
	if !ok {
		return false
	}
	// 检查当前时间是否在该时段内
	if nyTime.Before(start) || nyTime.After(end) {
		return false
	}

	maxDuration := time.Duration(n) * time.Minute
	elapsed := nyTime.Sub(start)

	return elapsed >= 0 && elapsed <= maxDuration
}

// IsInFirstNMinutesOfSession 判断当前时间是否在某个交易时段开始后的前 N 分钟内（使用 utils.Now()）
func (tc *TradingTimeControl) IsInFirstNMinutesOfSession(session TradingSession, n int) bool {
	return tc.IsInFirstNMinutesOfSessionAt(utils.Now(), session, n)
}

// IsInLastNMinutesOfSessionAt 判断指定时间是否在某个交易时段结束前的最后 N 分钟内
func (tc *TradingTimeControl) IsInLastNMinutesOfSessionAt(t time.Time, session TradingSession, n int) bool {
	if n <= 0 {
		return false
	}
	nyTime := t.In(tc.location)
	if tc.IsHoliday(nyTime) {
		return false
	}
	if nyTime.Weekday() == time.Saturday || nyTime.Weekday() == time.Sunday {
		return false
	}

	start, end, ok := tc.sessionTimeRange(nyTime, session)
	if !ok {
		return false
	}
	if nyTime.Before(start) || nyTime.After(end) {
		return false
	}

	maxDuration := time.Duration(n) * time.Minute
	remaining := end.Sub(nyTime)

	return remaining >= 0 && remaining <= maxDuration
}

// IsInLastNMinutesOfSession 判断当前时间是否在某个交易时段结束前的最后 N 分钟内
func (tc *TradingTimeControl) IsInLastNMinutesOfSession(session TradingSession, n int) bool {
	return tc.IsInLastNMinutesOfSessionAt(utils.Now(), session, n)
}

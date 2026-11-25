package strategy

import (
	"fmt"
	"sync"

	"LongBridge-QuantSystem/config"
	"LongBridge-QuantSystem/logger"
	"LongBridge-QuantSystem/trading"
	"LongBridge-QuantSystem/types"

	myconfig "LongBridge-QuantSystem/config"
)

// StrategyManager 策略管理器
type StrategyManager struct {
	mu       sync.RWMutex
	exitChan chan struct{}
	running  bool

	strategies      []Strategy
	strategiesMap   map[string]Strategy
	currentStrategy []Strategy // 改为切片，支持多策略同时运行
	appConfig       *config.AppConfig
	quantSystem     types.QuantSystemInterface
	cmdSymbol       string // 用于存储命令中的交易品种信息
	cmdSellInfo     string // 用于存储 sell 命令的参数信息
	cmdBuyInfo      string // 用于存储 buy 命令的参数信息
}

var (
	GlobalStrategyManager *StrategyManager
	once                  sync.Once
)

// GetStrategyManager 获取全局策略管理器
func GetStrategyManager(appConfig *config.AppConfig) *StrategyManager {
	once.Do(func() {
		GlobalStrategyManager = &StrategyManager{
			strategies:      make([]Strategy, 0),
			strategiesMap:   make(map[string]Strategy),
			currentStrategy: make([]Strategy, 0),
			appConfig:       appConfig,
			exitChan:        make(chan struct{}),

			cmdSymbol:   "",
			cmdSellInfo: "",
			cmdBuyInfo:  "",
		}
		// 注册所有策略
		GlobalStrategyManager.registerAllStrategies()
	})
	return GlobalStrategyManager
}

// registerAllStrategies 注册所有策略
func (m *StrategyManager) registerAllStrategies() {
	// 测试策略
	m.RegisterStrategy(NewTestStrategy())

	// 时间信号
	m.RegisterStrategy(NewTimeSignalStrategy())
	// 时间趋势
	m.RegisterStrategy(NewTimeTrendStrategy())

	// 网格策略
	m.RegisterStrategy(NewGridPercentStrategy()) // 百分比网格

	logger.Info("[StrategyManager] 已注册 %d 个策略", len(m.strategies))
}

// RegisterStrategy 注册策略
func (m *StrategyManager) RegisterStrategy(s Strategy) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := s.GetName()
	if _, exists := m.strategiesMap[name]; exists {
		logger.Warn("[StrategyManager] 策略已存在: %s", name)
		return
	}

	m.strategies = append(m.strategies, s)
	m.strategiesMap[name] = s
	logger.Info("[StrategyManager] 注册策略: %s", name)
}

// SetQuantSystem 设置量化系统
func (m *StrategyManager) SetQuantSystem(qs types.QuantSystemInterface) {
	m.quantSystem = qs
}

// GetQuantSystem 获取量化系统
func (m *StrategyManager) GetQuantSystem() types.QuantSystemInterface {
	return m.quantSystem
}

// GetStrategy 根据名称获取策略
func (m *StrategyManager) GetStrategy(name string) (Strategy, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.strategiesMap[name]
	return s, ok
}

// GetStrategyByIndex 根据索引获取策略
func (m *StrategyManager) GetStrategyByIndex(index int) (Strategy, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if index >= 0 && index < len(m.strategies) {
		return m.strategies[index], true
	}
	return nil, false
}

// GetAllStrategies 获取所有策略
func (m *StrategyManager) GetAllStrategies() []Strategy {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]Strategy, len(m.strategies))
	copy(result, m.strategies)
	return result
}

// GetStrategyCount 获取策略数量
func (m *StrategyManager) GetStrategyCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.strategies)
}

// SetCurrentStrategy 设置单个当前策略（兼容旧接口）
func (m *StrategyManager) SetCurrentStrategy(nameOrIndex string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var s Strategy
	var index int
	if _, err := fmt.Sscanf(nameOrIndex, "%d", &index); err == nil && index >= 1 && index <= len(m.strategies) {
		s = m.strategies[index-1]
	} else {
		var ok bool
		s, ok = m.strategiesMap[nameOrIndex]
		if !ok {
			return fmt.Errorf("策略不存在: %s", nameOrIndex)
		}
	}

	m.currentStrategy = []Strategy{s}
	return nil
}

// SetCurrentStrategies 设置多个当前策略
func (m *StrategyManager) SetCurrentStrategies(names []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	strategies := make([]Strategy, 0, len(names))
	for _, name := range names {
		s, ok := m.strategiesMap[name]
		if !ok {
			logger.Warn("[StrategyManager] 策略不存在，跳过: %s", name)
			continue
		}
		strategies = append(strategies, s)
	}

	if len(strategies) == 0 {
		return fmt.Errorf("没有有效的策略")
	}

	m.currentStrategy = strategies
	logger.Info("[StrategyManager] 启用 %d 个策略: %v", len(strategies), names)
	return nil
}

// SetCurrentStrategiesByType 根据类型设置当前策略
func (m *StrategyManager) SetCurrentStrategiesByType(strategyType string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	strategies := make([]Strategy, 0)
	for _, s := range m.strategies {
		if s.GetStrategyType() == strategyType {
			strategies = append(strategies, s)
		}
	}

	if len(strategies) == 0 {
		return fmt.Errorf("没有找到类型为 %s 的策略", strategyType)
	}

	m.currentStrategy = strategies
	logger.Info("[StrategyManager] 启用 %d 个 %s 类型策略", len(strategies), strategyType)
	return nil
}

// AddCurrentStrategy 添加策略到当前运行列表
func (m *StrategyManager) AddCurrentStrategy(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.strategiesMap[name]
	if !ok {
		return fmt.Errorf("策略不存在: %s", name)
	}

	// 检查是否已存在
	for _, existing := range m.currentStrategy {
		if existing.GetName() == name {
			logger.Warn("[StrategyManager] 策略已在运行列表中: %s", name)
			return nil
		}
	}

	m.currentStrategy = append(m.currentStrategy, s)
	logger.Info("[StrategyManager] 添加策略到运行列表: %s", name)
	return nil
}

// RemoveCurrentStrategy 从当前运行列表移除策略
func (m *StrategyManager) RemoveCurrentStrategy(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	newList := make([]Strategy, 0, len(m.currentStrategy))
	found := false
	for _, s := range m.currentStrategy {
		if s.GetName() == name {
			found = true
			continue
		}
		newList = append(newList, s)
	}

	if !found {
		return fmt.Errorf("策略不在运行列表中: %s", name)
	}

	m.currentStrategy = newList
	logger.Info("[StrategyManager] 从运行列表移除策略: %s", name)
	return nil
}

// GetCurrentStrategies 获取当前运行的策略列表
func (m *StrategyManager) GetCurrentStrategies() []Strategy {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]Strategy, len(m.currentStrategy))
	copy(result, m.currentStrategy)
	return result
}

// GetCurrentStrategyNames 获取当前运行的策略名称列表
func (m *StrategyManager) GetCurrentStrategyNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, len(m.currentStrategy))
	for i, s := range m.currentStrategy {
		names[i] = s.GetName()
	}
	return names
}

// GetCurrentStrategyCount 获取当前运行的策略数量
func (m *StrategyManager) GetCurrentStrategyCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.currentStrategy)
}

// InitStrategies 初始化所有当前策略
func (m *StrategyManager) InitStrategies() error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return nil
	}
	m.running = true
	m.mu.Unlock()

	if len(m.currentStrategy) == 0 {
		return fmt.Errorf("没有启用的策略")
	}

	for _, s := range m.currentStrategy {
		if err := s.Init(m.appConfig); err != nil {
			logger.Error("[StrategyManager] 初始化策略失败 %s: %v", s.GetName(), err)
			return err
		}
		logger.Info("[StrategyManager] 初始化策略成功: %s", s.GetName())
	}
	return nil
}

// GetSignal 使用所有当前策略获取信号（合并所有策略的订单）
func (m *StrategyManager) GetSignal(symbol string) ([]types.OrderTarget, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.currentStrategy) == 0 {
		return nil, fmt.Errorf("没有启用的策略")
	}

	allOrders := make([]types.OrderTarget, 0)
	var lastErr error

	for _, s := range m.currentStrategy {
		orders, err := s.GetSignal(symbol)
		if err != nil {
			logger.Warn("[StrategyManager] 策略 %s 获取信号失败: %v", s.GetName(), err)
			lastErr = err
			continue
		}
		if len(orders) > 0 {
			logger.Info("[StrategyManager] 策略 %s 生成 %d 个订单", s.GetName(), len(orders))
			allOrders = append(allOrders, orders...)
		}
	}

	if m.cmdSymbol != "" {
		if m.cmdSellInfo != "" {
			logger.Warn("[StrategyManager] 处理 sell 股票：%s 指令：%s", m.cmdSymbol, m.cmdSellInfo)
			for _, s := range m.currentStrategy {
				logger.Warn("[StrategyManager] 处理 sell 股票：%s 指令：%s", m.cmdSymbol, m.cmdSellInfo)
				orders := s.CmdSell(m.cmdSymbol, m.cmdSellInfo)
				if len(orders) > 0 {
					logger.Info("[StrategyManager] 策略 %s 指令卖出生成 %d 个订单", s.GetName(), len(orders))
					allOrders = append(allOrders, orders...)
				}
			}
			m.cmdSellInfo = ""
		} else if m.cmdBuyInfo != "" {
			logger.Warn("[StrategyManager] 处理 buy 股票：%s 指令：%s", m.cmdSymbol, m.cmdBuyInfo)
			for _, s := range m.currentStrategy {
				logger.Warn("[StrategyManager] 处理 buy 股票：%s 指令：%s", m.cmdSymbol, m.cmdBuyInfo)
				orders := s.CmdBuy(m.cmdSymbol, m.cmdBuyInfo)
				if len(orders) > 0 {
					logger.Info("[StrategyManager] 策略 %s 指令买入生成 %d 个订单", s.GetName(), len(orders))
					allOrders = append(allOrders, orders...)
				}
			}
			m.cmdBuyInfo = ""
		}

		m.cmdSymbol = ""
	}

	return allOrders, lastErr
}

// GetStrategyConfigs 获取所有当前策略的配置
func (m *StrategyManager) GetStrategyConfigs() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	configs := make(map[string]interface{})
	configs["strategies"] = m.GetCurrentStrategyNames()
	configs["count"] = len(m.currentStrategy)

	strategyConfigs := make([]map[string]interface{}, 0)
	for _, s := range m.currentStrategy {
		strategyConfigs = append(strategyConfigs, s.GetStrategyConfig())
	}
	configs["details"] = strategyConfigs

	return configs
}

// GetCurrentStrategyInfo 获取当前策略信息（兼容旧接口）
func (m *StrategyManager) GetCurrentStrategyInfo() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.currentStrategy) == 0 {
		return "未选择策略"
	}

	names := m.GetCurrentStrategyNames()
	return fmt.Sprintf("运行 %d 个策略: %v", len(names), names)
}

// DispatchOrderEvent 分发订单事件到所有当前策略
func (m *StrategyManager) DispatchOrderEvent(eventType string, orderID string, order *types.OrderTarget) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, s := range m.currentStrategy {
		if s.GetName() != order.StrategyName {
			continue
		}
		switch eventType {
		case "BuySubmitted":
			s.OnBuyOrderSubmitted(orderID, order)
		case "BuyFilled":
			s.OnBuyOrderFilled(orderID, order)
		case "SellSubmitted":
			s.OnSellOrderSubmitted(orderID, order)
		case "SellFilled":
			s.OnSellOrderFilled(orderID, order)
		case "OrderFailed":
			s.OnOrderFailed(orderID, order)
		}
	}
}

func (m *StrategyManager) CanExit() bool {
	if myconfig.IsTestMode() {
		return false
	}

	control := trading.GetTradingTimeControl()
	if control == nil {
		logger.Error("control == nil")
		return false
	}
	currentSession := control.GetCurrentSession()
	if currentSession == trading.SessionOvernight {
		return m.appConfig.StrategyOvernightExitFirstMinute > 0 && control.IsInFirstNMinutesOfSession(currentSession, m.appConfig.StrategyOvernightExitFirstMinute)
	}

	return false
}

func (m *StrategyManager) CanSessionOrder() bool {
	control := trading.GetTradingTimeControl()
	if control == nil {
		logger.Error("control == nil")
		return false
	}
	currentSession := control.GetCurrentSession()
	switch currentSession {
	case trading.SessionPre:
		if (m.appConfig.StrategySessionPreFirstMinute > 0 && control.IsInFirstNMinutesOfSession(currentSession, m.appConfig.StrategySessionPreFirstMinute)) ||
			(m.appConfig.StrategySessionPreLastMinute > 0 && control.IsInLastNMinutesOfSession(currentSession, m.appConfig.StrategySessionPreLastMinute)) {
			return false
		}
		return true
	case trading.SessionRegular:
		if (m.appConfig.StrategySessionRegularFirstMinute > 0 && control.IsInFirstNMinutesOfSession(currentSession, m.appConfig.StrategySessionRegularFirstMinute)) ||
			(m.appConfig.StrategySessionRegularLastMinute > 0 && control.IsInLastNMinutesOfSession(currentSession, m.appConfig.StrategySessionRegularLastMinute)) {
			return false
		}
		return true
	case trading.SessionAfter:
		if (m.appConfig.StrategySessionAfterFirstMinute > 0 && control.IsInFirstNMinutesOfSession(currentSession, m.appConfig.StrategySessionAfterFirstMinute)) ||
			(m.appConfig.StrategySessionAfterLastMinute > 0 && control.IsInLastNMinutesOfSession(currentSession, m.appConfig.StrategySessionAfterLastMinute)) {
			return false
		}
		return true
	}

	return false
}

func (m *StrategyManager) GetQuantity() int64 {
	control := trading.GetTradingTimeControl()
	if control == nil {
		logger.Error("control == nil")
		return 0
	}
	currentSession := control.GetCurrentSession()
	switch currentSession {
	case trading.SessionPre:
		return m.appConfig.StrategyPreQuantityStep
	case trading.SessionAfter:
		return m.appConfig.StrategyAfterQuantityStep
	case trading.SessionRegular:
		if (m.appConfig.StrategyRegularFirstMinute > 0 && control.IsInFirstNMinutesOfSession(currentSession, m.appConfig.StrategyRegularFirstMinute)) ||
			(m.appConfig.StrategyRegularAfterMinute > 0 && control.IsInLastNMinutesOfSession(currentSession, m.appConfig.StrategyRegularAfterMinute)) {
			return m.appConfig.StrategyRegularQuantityStep / 2
		}
		return m.appConfig.StrategyRegularQuantityStep
	}

	return 0
}

// ResetAllStrategies 重置所有当前策略
func (m *StrategyManager) ResetAllStrategies() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, s := range m.currentStrategy {
		s.Reset()
		logger.Info("[StrategyManager] 重置策略: %s", s.GetName())
	}
}

// Stop 停止
func (m *StrategyManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return
	}
	m.running = false

	close(m.exitChan)
	logger.Info("停止策略")
}

// 获取退出信号通道
func (m *StrategyManager) GetExitChan() <-chan struct{} {
	return m.exitChan
}

// SetCmdSell 设置 sell 命令的信息
func (m *StrategyManager) SetCmdSell(symbol string, info string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cmdSymbol = symbol
	m.cmdSellInfo = info
}

// SetCmdBuy 设置 buy 命令的信息
func (m *StrategyManager) SetCmdBuy(symbol string, info string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cmdSymbol = symbol
	m.cmdBuyInfo = info
}

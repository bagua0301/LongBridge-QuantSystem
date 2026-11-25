package quant

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	myconfig "LongBridge-QuantSystem/config"
	"LongBridge-QuantSystem/logger"
	"LongBridge-QuantSystem/strategy"
	"LongBridge-QuantSystem/strategy_t"
	"LongBridge-QuantSystem/trading"
	"LongBridge-QuantSystem/types"
	"LongBridge-QuantSystem/utils"

	"github.com/joho/godotenv"
	"github.com/longportapp/openapi-go/config"
	"github.com/longportapp/openapi-go/quote"
	"github.com/longportapp/openapi-go/trade"
)

// IsRunning 是否运行中
func (qs *QuantSystem) IsRunning() bool {
	qs.runLock.RLock()
	defer qs.runLock.RUnlock()
	return qs.running
}

// Stop 停止系统
func (qs *QuantSystem) Stop() {
	qs.runLock.Lock()
	defer qs.runLock.Unlock()
	qs.running = false
	logger.Warn("系统停止")
}

// NewQuantSystem 创建量化系统
func NewQuantSystem() (*QuantSystem, error) {
	// 1. 加载应用配置
	appConfig := myconfig.GetConfig()
	if appConfig == nil {
		return nil, fmt.Errorf("appConfig == nil")
	}

	// 设置全局时区
	myLoc, err := time.LoadLocation(appConfig.TimeZone)
	if err != nil {
		return nil, fmt.Errorf("警告: 加载时区失败: %v, 使用系统时区", err)
	}
	time.Local = myLoc

	utils.VirtualClockInit()
	var timeScale float64 = 1
	if myconfig.IsTestMode() {
		timeScale = appConfig.TestTimeScale
	}
	utils.VirtualClockSetScale(timeScale)

	// 2. 初始化日志系统
	if err := logger.InitLogger(appConfig); err != nil {
		return nil, fmt.Errorf("init logger failed: %v", err)
	}

	logger.Warn("时间因子：%.2f", timeScale)

	// 3. 显示时区信息
	displayTimeZoneInfo()

	var tradeCtx *trade.TradeContext
	var quoteCtx *quote.QuoteContext
	if !myconfig.IsTestMode() {
		// 4. 加载长桥配置
		if err := godotenv.Load("config.env"); err != nil {
			logger.Warn("Could not load config.env: %v", err)
		}

		// 5. 创建长桥配置
		cfg, err := config.New()
		if err != nil {
			return nil, fmt.Errorf("load longbridge config failed: %v", err)
		}

		// 6. 创建交易上下文
		tradeCtx, err = trade.NewFromCfg(cfg)
		if err != nil {
			return nil, fmt.Errorf("create trade context failed: %v", err)
		}

		// 7. 创建行情上下文
		quoteCtx, err = quote.NewFromCfg(cfg)
		if err != nil {
			return nil, fmt.Errorf("create quote context failed: %v", err)
		}
	}

	// 8. 获取策略管理器并设置默认策略
	strategyMgr := strategy.GetStrategyManager(appConfig)
	strategyType := appConfig.StrategyType
	if len(strategyType) == 0 {
		strategyType = []string{"GridPercent"}
	}
	if err := strategyMgr.SetCurrentStrategies(strategyType); err != nil {
		logger.Fatal("设置默认策略失败: %v, 使用 GridPercent", err)
		strategyMgr.SetCurrentStrategy("GridPercent")
	}

	// 10. 获取测试策略管理器并设置默认测试策略
	strategyTestMgr := strategy_t.GetTestStrategyManager(appConfig)
	TestStrategy := appConfig.TestStrategy
	if TestStrategy == "" {
		TestStrategy = "price"
	}
	if err := strategyTestMgr.SetCurrentStrategy(TestStrategy); err != nil {
		logger.Fatal("设置默认测试策略失败: %v, 使用 price", err)
		strategyTestMgr.SetCurrentStrategy("price")
	}

	// 创建交易时间控制
	tradeControl := trading.CreateTradingTimeControl(myLoc, appConfig.TradingSessions)
	ctx, cancel := context.WithCancel(context.Background())
	qs := &QuantSystem{
		ctx:             ctx,
		cancel:          cancel,
		TradeControl:    tradeControl,
		TradeCtx:        tradeCtx,
		QuoteCtx:        quoteCtx,
		AppConfig:       appConfig,
		PriceManager:    NewPriceManager(appConfig),
		StrategyMgr:     strategyMgr,
		StrategyTestMgr: strategyTestMgr,
		Positions:       make(map[string]*types.PositionInfo),
		CompletedOrders: make(map[string]*types.CompletedOrder, 0),
	}
	// 初始化配置字段映射
	qs.initConfigFields()

	// 创建订单查询管理器
	qs.OrderQuery = NewOrderQuery(qs)

	strategyMgr.SetQuantSystem(qs)
	// 要在NewPriceUpdater后
	strategyMgr.InitStrategies()

	strategyTestMgr.SetQuantSystem(qs)

	logger.Warn("=========== 配置信息 ===========")
	log := qs.GetAllFlagConfigField("Log")
	for k, v := range log {
		logger.Warn("Log信息: %s=%v", k, v)
	}
	com := qs.GetAllFlagConfigField("Com")
	for k, v := range com {
		logger.Warn("公共信息: %s=%v", k, v)
	}
	allGetCurrentStrategies := qs.StrategyMgr.GetCurrentStrategies()
	for _, s := range allGetCurrentStrategies {
		name := s.GetName()
		logger.Warn("================================%s begin", name)
		data := s.GetStrategyConfig()
		for k, v := range data {
			logger.Warn("策略信息: %s=%v", k, v)
		}
		strategyCom := qs.GetAllFlagConfigField(fmt.Sprintf("%sCom", data["shortName"]))
		for k, v := range strategyCom {
			logger.Warn("策略公共信息: %s=%v", k, v)
		}
		strategyData := qs.GetAllFlagConfigField(fmt.Sprintf("%s", data["shortName"]))
		for k, v := range strategyData {
			logger.Warn("策略当前信息: %s=%v", k, v)
		}
		if data["strategyType"] != "" {
			strategyAllData := qs.GetAllFlagConfigField(fmt.Sprintf("%s%s", data["shortName"], data["strategyType"]))
			for k, v := range strategyAllData {
				logger.Warn("策略当前信息: %s=%v", k, v)
			}
		}
		logger.Warn("================================%s end", name)
	}
	logger.Warn("================================")

	// 显示当前交易时段信息
	displayCurrentSessionInfo(qs.TradeControl)

	return qs, nil
}

// displayTimeZoneInfo 显示时区信息
func displayTimeZoneInfo() {
	logger.Warn("=========== 时区信息 ===========")
	timeInfo := utils.GetTimeInfo()
	logger.Warn("北京时间: %s", timeInfo["beijing_time"])
	logger.Warn("美东时间: %s", timeInfo["newyork_time"])
	logger.Warn("时差: %s", timeInfo["timezone_diff"])
	logger.Warn("夏令时: %v", timeInfo["is_daylight_saving"])
	logger.Warn("================================")
}

// displayCurrentSessionInfo 显示当前交易时段信息
func displayCurrentSessionInfo(tc *trading.TradingTimeControl) {
	logger.Warn("========= 交易时段信息 =========")
	timeInfo := utils.GetTimeInfo()
	info := tc.GetCurrentTimeInfo()
	logger.Warn("北京时间: %s", timeInfo["beijing_time"])
	logger.Warn("美东时间: %s", timeInfo["newyork_time"])
	logger.Warn("时差: %s", timeInfo["timezone_diff"])
	logger.Warn("当前时段: %s", info["session"])
	logger.Warn("是否允许交易: %v", info["is_allowed"])
	logger.Warn("================================")
}

// Run 运行量化系统
func (qs *QuantSystem) Run() {
	qs.runLock.Lock()
	qs.running = true
	qs.runLock.Unlock()

	logger.Warn("量化系统启动...")
	logger.Warn("交易时段设置: %s", qs.TradeControl.GetAllowedSessionsDesc())
	logger.Warn("监控股票数量: %d", len(qs.AppConfig.Symbols))

	defer qs.cancel()

	// ============================================
	// 1. 先注册回调函数
	// ============================================
	if !myconfig.IsTestMode() {
		// 行情回调
		qs.QuoteCtx.OnQuote(func(ev *quote.PushQuote) {
			if ev == nil {
				return
			}
			qs.PriceManager.OnQuote(ev)

			curTime := utils.Now().Unix()
			last := qs.lastOrderLogicTimestamp.Load()
			if last+qs.AppConfig.StrategyOrderLogicSec <= curTime {
				qs.lastOrderLogicTimestamp.Store(curTime)
				// 订单逻辑
				qs.OrderLogic()
			}
		})
	} else {
		logger.Warn("测试模式,未监控行情回调")
	}

	// // 交易回调
	// qs.TradeCtx.OnTrade(func(ev *trade.PushEvent) {
	// 	if ev == nil {
	// 		return
	// 	}
	// 	bytes, _ := json.Marshal(ev)
	// 	logger.Debug("OnTrade 原始事件: %s", string(bytes))
	// 	qs.handleTradeEvent(ev)
	// })

	// ============================================
	// 2. 订阅行情
	// ============================================
	if !myconfig.IsTestMode() {
		if err := qs.QuoteCtx.Subscribe(qs.ctx, qs.AppConfig.Symbols, []quote.SubType{quote.SubTypeQuote}, true); err != nil {
			logger.Fatal("subscribe quote failed: %v", err)
		}
		logger.Warn("订阅行情成功，股票: %v", qs.AppConfig.Symbols)

		// ============================================
		// 3. 获取账户资产
		// ============================================
		if accounts, err := qs.TradeCtx.AccountBalance(qs.ctx); err != nil {
			logger.Warn("account balance query failed (non-fatal): %v", err)
		} else {
			logger.Warn("account balance ok, accounts=%d", len(accounts))
			for _, acc := range accounts {
				if acc.TotalCash != nil {
					logger.Warn("账户总资金: %s", acc.TotalCash.String())
				}
				if acc.NetAssets != nil {
					logger.Warn("净资产: %s", acc.NetAssets.String())
				}
				logger.Warn("货币: %s, 风险等级: %s", acc.Currency, acc.RiskLevel)
			}
		}

		// ============================================
		// 4. 获取初始持仓（只记录，不参与策略统计）
		// ============================================
		// qs.loadInitialPositions(qs.ctx)
		// qs.loadInitialFundPositions(qs.ctx)
		// logger.Warn("初始持仓加载完成，当前持仓数量: %d", len(qs.Positions))
	}

	// ============================================
	// 5. 启动控制台命令处理
	// ============================================
	go qs.handleConsoleCommands(qs.ctx)

	// ============================================
	// 6. 等待一小段时间，确保行情推送稳定
	// ============================================
	time.Sleep(2 * time.Second)
	logger.Warn("系统初始化完成，开始执行策略...")

	// ============================================
	// 7. 启动主循环
	// ============================================
	go qs.mainLoop(qs.ctx)

	if myconfig.IsTestMode() {
		qs.StrategyTestMgr.Start()
	}

	// ============================================
	// 8. 等待退出信号
	// ============================================
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// 创建退出等待通道
	exitChan := make(chan struct{})
	if myconfig.IsTestMode() {
		go func() {
			<-qs.StrategyTestMgr.GetExitChan()
			logger.Warn("测试完成，准备退出")
			close(exitChan)
		}()
	} else {
		go func() {
			<-qs.StrategyMgr.GetExitChan()
			logger.Warn("停止策略，准备退出")
			close(exitChan)
		}()
	}

	// 等待退出信号（手动退出或测试完成）
	select {
	case <-quit:
		logger.Warn("收到系统退出信号")
	case <-exitChan:
		logger.Warn("测试完成，自动退出")
	}

	logger.Warn("程序退出...")
	qs.printAllSummary()
	qs.Stop()
}

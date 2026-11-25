package quant

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"

	myconfig "LongBridge-QuantSystem/config"
	"LongBridge-QuantSystem/logger"
	"LongBridge-QuantSystem/strategy"
	"LongBridge-QuantSystem/types"
	"LongBridge-QuantSystem/utils"
)

// 在反射时检查标签
func isConfigurableRO(field reflect.StructField, flag string) bool {
	return field.Tag.Get("config") == flag
}

// handleConsoleCommands 处理控制台命令
func (qs *QuantSystem) handleConsoleCommands(ctx context.Context) {
	scanner := bufio.NewScanner(os.Stdin)
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if scanner.Scan() {
				cmd := scanner.Text()
				qs.processCommand(cmd)
			}
		}
	}
}

// processCommand 处理控制台命令
func (qs *QuantSystem) processCommand(cmd string) {
	logger.Warn("指令：%s", cmd)
	if strings.HasPrefix(cmd, "strategy_test") {
		parts := strings.Fields(cmd)
		if len(parts) >= 2 {
			if qs.StrategyTestMgr == nil {
				logger.Info("未开启策略测试")
				return
			}
			strategyInput := parts[1]

			// 获取所有策略列表
			allStrategies := qs.StrategyTestMgr.GetAllStrategies()
			// 尝试将输入解析为数字（策略编号）
			if num, err := strconv.Atoi(strategyInput); err == nil {
				if num >= 1 && num <= len(allStrategies) {
					strategyName := allStrategies[num-1].GetName()
					if err := qs.StrategyTestMgr.SetCurrentStrategy(strategyName); err != nil {
						logger.Error("切换策略测试失败: %v", err)
						qs.listStrategiesTest()
						return
					}
					qs.StrategyTestMgr.Start()
					logger.Info("策略测试已切换到: %s", qs.StrategyTestMgr.GetCurrentStrategyInfo())
				} else {
					logger.Error("无效的策略编号: %d，可用范围 1-%d", num, len(allStrategies))
					qs.listStrategiesTest()
				}
				return
			}

			// 输入是策略名称，直接切换
			if err := qs.StrategyTestMgr.SetCurrentStrategy(strategyInput); err != nil {
				logger.Error("切换策略测试失败: %v", err)
				qs.listStrategiesTest()
				return
			}
			qs.StrategyTestMgr.Start()
			logger.Info("策略测试已切换到: %s", qs.StrategyTestMgr.GetCurrentStrategyInfo())
		} else {
			logger.Info("用法: strategy_test gridfixed")
			qs.listStrategiesTest()
		}
	} else if strings.HasPrefix(cmd, "strategy") {
		parts := strings.Fields(cmd)
		if len(parts) >= 2 {
			strategyInput := parts[1]

			// 获取所有策略列表
			allStrategies := qs.StrategyMgr.GetAllStrategies()

			// 尝试将输入解析为数字（策略编号）
			if num, err := strconv.Atoi(strategyInput); err == nil {
				if num >= 1 && num <= len(allStrategies) {
					strategyName := allStrategies[num-1].GetName()
					if err := qs.StrategyMgr.SetCurrentStrategy(strategyName); err != nil {
						logger.Error("切换策略失败: %v", err)
						qs.listStrategies()
						return
					}
					qs.StrategyMgr.InitStrategies()
					logger.Info("策略已切换到: %s", qs.StrategyMgr.GetCurrentStrategyInfo())
				} else {
					logger.Error("无效的策略编号: %d，可用范围 1-%d", num, len(allStrategies))
					qs.listStrategies()
				}
				return
			}

			// 输入是策略名称，直接切换
			if err := qs.StrategyMgr.SetCurrentStrategy(strategyInput); err != nil {
				logger.Error("切换策略失败: %v", err)
				qs.listStrategies()
				return
			}
			qs.StrategyMgr.InitStrategies()
			logger.Info("策略已切换到: %s", qs.StrategyMgr.GetCurrentStrategyInfo())
		} else {
			qs.listStrategies()
		}
	} else if strings.HasPrefix(cmd, "set") {
		// 处理 set 命令
		qs.handleSetCommand(cmd)
	} else if strings.HasPrefix(cmd, "get") {
		// 处理 get 命令
		qs.handleGetCommand(cmd)
	} else if strings.HasPrefix(cmd, "sell") {
		parts := strings.Fields(cmd)
		if len(parts) >= 3 {
			qs.StrategyMgr.SetCmdSell(parts[1], parts[2])
		} else {
			logger.Info("========================================")
			logger.Info("  sell SOXL.US all        - 主动卖出所有股票")
			logger.Info("  sell SOXL.US win        - 主动卖出盈利股票")
			logger.Info("  sell SOXL.US lost       - 主动卖出亏损股票")
			logger.Info("========================================")
		}
	} else if strings.HasPrefix(cmd, "buy") {
		parts := strings.Fields(cmd)
		if len(parts) >= 3 {
			qs.StrategyMgr.SetCmdBuy(parts[1], parts[2])
		} else {
			logger.Info("========================================")
			logger.Info("  buy SOXL.US 100         - 主动买入股票")
			logger.Info("========================================")
		}
	} else if cmd == "help" {
		qs.printHelp()
	} else if cmd == "status" {
		qs.printStatus()
	} else if cmd == "ss" {
		allGetCurrentStrategies := qs.StrategyMgr.GetCurrentStrategies()
		for _, s := range allGetCurrentStrategies {
			// 显示策略内部状态
			qs.printStrategyState(s)
		}
	} else if cmd == "pending" {
		// 显示待确认订单
		qs.printPendingOrders()
	} else if cmd == "macd" {
		parts := strings.Fields(cmd)
		if len(parts) >= 3 {
			allOrders := make([]*types.CompletedOrder, 0, len(qs.CompletedOrders))
			for _, order := range qs.CompletedOrders {
				allOrders = append(allOrders, order)
			}
			sort.Slice(allOrders, func(i, j int) bool {
				return allOrders[i].Timestamp.Before(allOrders[j].Timestamp)
			})
			qs.PriceManager.DrawMACD(parts[1], "logs/macd_"+parts[2]+".png", allOrders)
		}
	} else if cmd == "price" {
		parts := strings.Fields(cmd)
		if len(parts) >= 3 {
			allOrders := make([]*types.CompletedOrder, 0, len(qs.CompletedOrders))
			for _, order := range qs.CompletedOrders {
				allOrders = append(allOrders, order)
			}
			sort.Slice(allOrders, func(i, j int) bool {
				return allOrders[i].Timestamp.Before(allOrders[j].Timestamp)
			})
			qs.PriceManager.DrawPrice(parts[1], "logs/price_"+parts[2]+".png", allOrders)
		}
	} else if cmd == "exit" {
		if myconfig.IsTestMode() {
			qs.StrategyTestMgr.Stop()
		} else {
			qs.StrategyMgr.Stop()
		}
	}
}

// listStrategies 列出所有可用策略
func (qs *QuantSystem) listStrategies() {
	logger.Info("========================================")
	logger.Info("可用策略:")

	allStrategiesNames := qs.StrategyMgr.GetCurrentStrategyNames()
	for i, name := range allStrategiesNames {
		logger.Info("  %d. %s", i+1, name)
	}

	logger.Info("========================================")
	logger.Info("使用 'strategy [编号]' 或 'strategy [名称]' 切换策略")
	logger.Info("========================================")
}

// listStrategiesTest 列出所有可用策略测试
func (qs *QuantSystem) listStrategiesTest() {
	logger.Info("========================================")
	logger.Info("可用策略测试:")

	allStrategies := qs.StrategyTestMgr.GetAllStrategies()
	currentStrategy := qs.StrategyTestMgr.GetCurrentStrategy()

	for i, s := range allStrategies {
		current := ""
		if currentStrategy != nil && currentStrategy.GetName() == s.GetName() {
			current = " [当前]"
		}
		logger.Info("  %d. %s - %s%s", i+1, s.GetName(), s.GetDescription(), current)
	}

	logger.Info("========================================")
	logger.Info("使用 'strategy_test [名称]' 切换策略")
	logger.Info("========================================")
}

// printHelp 打印帮助信息
func (qs *QuantSystem) printHelp() {
	logger.Info("========================================")
	logger.Info("可用命令:")
	logger.Info("  strategy [编号]         - 按编号切换策略")
	logger.Info("  strategy [名称]         - 按名称切换策略")
	logger.Info("  strategy                - 列出所有策略")
	logger.Info("  strategy_test GridPercent - 启动网格策略测试")
	logger.Info("  ss                      - 显示当前策略内部状态")
	logger.Info("  pending                 - 显示待确认订单")
	logger.Info("  status                  - 显示系统状态")
	logger.Info("  help                    - 显示帮助")
	logger.Info("  sell SOXL.US all        - 主动卖出所有股票")
	logger.Info("  sell SOXL.US win        - 主动卖出盈利股票")
	logger.Info("  sell SOXL.US lost       - 主动卖出亏损股票")
	logger.Info("  buy SOXL.US 100         - 主动买入股票")
	logger.Info("  macd SOXL.US test       - 生成macd图片(test.png)")
	logger.Info("========================================")
}

// printStatus 打印系统状态
func (qs *QuantSystem) printStatus() {
	logger.Info("========================================")
	logger.Info("【系统状态】")
	logger.Info("当前策略: %s", qs.StrategyMgr.GetCurrentStrategyInfo())
	info := qs.TradeControl.GetCurrentTimeInfo()
	logger.Info("当前时段: %s", info["session"])
	logger.Info("是否允许交易: %v", info["is_allowed"])
	logger.Info("今日已交易: %d/%d", qs.DailyTradeCount, qs.AppConfig.MaxDailyTrades)
	logger.Info("持仓数量: %d", len(qs.Positions))

	// 显示待确认订单数量
	pendingCount := qs.OrderQuery.GetPendingOrderCount()
	if pendingCount > 0 {
		logger.Info("待确认订单: %d", pendingCount)
	}

	logger.Info("========================================")
}

// printStrategyState 打印策略内部状态
func (qs *QuantSystem) printStrategyState(currentStrategy strategy.Strategy) {
	if currentStrategy == nil {
		logger.Warn("当前没有活动策略")
		return
	}

	logger.Info("========================================")
	logger.Info("【策略状态】")
	logger.Info("策略名称: %s", currentStrategy.GetName())
	logger.Info("策略描述: %s", currentStrategy.GetDescription())

	// 显示当前价格
	for _, v := range qs.AppConfig.Symbols {
		logger.Info("%s 当前价格: %.4f", v, qs.PriceManager.GetCurrentPrice(v))
	}

	// 获取策略状态
	state := currentStrategy.GetCurrentState()
	logger.Info("【策略详细状态】")
	for k, v := range state {
		if k != "orders" {
			logger.Info("%s: %v", k, v)
		}
	}

	var allProfit, allFloatingProfit float64 = 0, 0
	if orders, ok := state["orders"]; ok {
		if orderList, ok := orders.([]map[string]interface{}); ok && len(orderList) > 0 {
			// 分离已成交和未成交订单
			var soldOrders, unsoldOrders []map[string]interface{}
			for _, order := range orderList {
				if remaining, ok := order["remaining"].(int64); ok && remaining <= 0 {
					soldOrders = append(soldOrders, order)
				} else {
					unsoldOrders = append(unsoldOrders, order)
				}
			}

			// 打印已成交订单
			if len(soldOrders) > 0 {
				logger.Info("========================================")
				logger.Info("已成交订单明细:")
				for i, order := range soldOrders {
					// 添加类型断言，避免 panic
					orderID, _ := order["order_id"].(string)
					price, _ := order["price"].(float64)
					quantity, _ := order["quantity"].(int64)
					remaining, _ := order["remaining"].(int64)
					profit, _ := order["profit"].(float64)
					closeOrderID, _ := order["close_order_id"].(string)
					logger.Info("    [%d] 订单ID=%s, 买入价=%.4f, 数量=%d, 剩余数量=%d, 利润=%.4f, 绑定订单=%s",
						i+1, orderID, price, quantity, remaining, profit, closeOrderID)

					allProfit += profit
				}
				logger.Info("========================================")
			}

			// 打印未成交订单
			if len(unsoldOrders) > 0 {
				logger.Info("========================================")
				logger.Info("未成交订单明细:")
				for i, order := range unsoldOrders {
					// 添加类型断言
					symbol, _ := order["symbol"].(string)
					orderID, _ := order["order_id"].(string)
					price, _ := order["price"].(float64)
					quantity, _ := order["quantity"].(int64)
					remaining, _ := order["remaining"].(int64)
					profit, _ := order["profit"].(float64)

					logger.Info("    [%d] 订单ID=%s, 买入价=%.4f, 数量=%d, 剩余数量=%d",
						i+1, orderID, price, quantity, remaining)

					currentPrice := qs.PriceManager.GetCurrentPrice(symbol)

					allFloatingProfit += profit + float64(remaining)*(currentPrice-price)
				}
				logger.Info("========================================")
			}
		}
	}
	logger.Info("allProfit==%.2f, allFloatingProfit=%.2f total=%.2f", allProfit, allFloatingProfit, allProfit+allFloatingProfit)
}

// printPendingOrders 打印待确认订单
func (qs *QuantSystem) printPendingOrders() {
	pendingOrders := qs.OrderQuery.GetPendingOrders()

	if len(pendingOrders) == 0 {
		logger.Info("没有待确认订单")
		return
	}

	logger.Info("========================================")
	logger.Info("【待确认订单】共 %d 个", len(pendingOrders))
	logger.Info("┌─────────────────────────────────────────────────────────────────┐")
	logger.Info("│ 订单ID                 │ 股票   │ 方向 │ 数量 │ 价格   │ 等待时间 │")
	logger.Info("├─────────────────────────────────────────────────────────────────┤")

	for _, pending := range pendingOrders {
		waitTime := utils.Since(pending.SubmitTime).Seconds()
		logger.Info("│ %-20s │ %-6s │ %-4s │ %4d │ %6.2f │ %8.0fs │",
			pending.Order.OrderID, pending.Order.Symbol, pending.Order.Action, pending.Order.Quantity, pending.Order.TargetPrice, waitTime)
	}
	logger.Info("└─────────────────────────────────────────────────────────────────┘")
	logger.Info("========================================")
}

// handleSetCommand 使用反射自动处理配置修改
func (qs *QuantSystem) handleSetCommand(cmd string) {
	parts := strings.Fields(cmd)
	if len(parts) < 3 {
		logger.Info("用法: set <配置项> <值>")
		logger.Info("可用配置项:")
		qs.listConfigFields()
		return
	}

	key := strings.ToLower(parts[1])
	value := strings.Join(parts[2:], " ")

	// 使用反射设置配置值
	if ok, err := qs.setConfigField(key, value); !ok {
		logger.Error("设置失败: %v", err)
		logger.Info("可用配置项:")
		qs.listConfigFields()
	} else {
		logger.Info("✓ 指令设置配置 %s", cmd)
		logger.Info("✓ 配置已更新 %v", err)
	}
}

// handleGetCommand 使用反射自动处理获取配置
func (qs *QuantSystem) handleGetCommand(cmd string) {
	parts := strings.Fields(cmd)
	if len(parts) < 2 {
		logger.Info("用法: get <配置项>")
		logger.Info("可用配置项:")
		qs.listConfigFields()
		return
	}

	key := strings.ToLower(parts[1])

	// 获取配置值
	value, err := qs.getConfigField(key)
	if err != nil {
		logger.Error("获取配置失败: %v", err)
		logger.Info("可用配置项:")
		qs.listConfigFields()
	} else {
		logger.Info("✓ 指令获取配置成功 %s", cmd)
		logger.Info("✓ 指令获取配置 %s = %v", key, value)
	}
}

// listConfigFields 列出所有可配置字段
func (qs *QuantSystem) listConfigFields() {
	configValue := reflect.ValueOf(qs.AppConfig).Elem()
	configType := configValue.Type()

	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		fieldValue := configValue.Field(i)

		// 跳过私有字段
		if field.PkgPath != "" {
			continue
		}

		if isConfigurableRO(field, "ro") {
			continue
		}

		// 格式化显示
		fieldName := field.Name
		var typeStr string
		var currentValue string

		switch fieldValue.Kind() {
		case reflect.String:
			typeStr = "string"
			currentValue = fieldValue.String()
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			typeStr = "int"
			currentValue = strconv.FormatInt(fieldValue.Int(), 10)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			typeStr = "uint"
			currentValue = strconv.FormatUint(fieldValue.Uint(), 10)
		case reflect.Float32, reflect.Float64:
			typeStr = "float"
			currentValue = strconv.FormatFloat(fieldValue.Float(), 'f', -1, 64)
		case reflect.Bool:
			typeStr = "bool"
			currentValue = strconv.FormatBool(fieldValue.Bool())
		default:
			continue
		}

		logger.Info("  %-20s - %s (当前: %s)", fieldName, typeStr, currentValue)
	}
}

// initConfigFields 初始化配置字段映射
func (qs *QuantSystem) initConfigFields() {
	qs.configFields = make(map[string]configFieldInfo)

	configValue := reflect.ValueOf(qs.AppConfig).Elem()
	configType := configValue.Type()

	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)

		// 跳过私有字段
		if field.PkgPath != "" {
			continue
		}
		if isConfigurableRO(field, "ro") {
			continue
		}

		// 添加多种命名方式的映射
		fieldName := field.Name
		snakeName := utils.ToSnakeCase(fieldName)
		lowerName := strings.ToLower(fieldName)

		info := configFieldInfo{
			FieldName:  fieldName,
			FieldIndex: i,
			FieldType:  field.Type.Kind(),
		}

		qs.configFields[fieldName] = info
		qs.configFields[snakeName] = info
		qs.configFields[lowerName] = info
	}
}

// setFieldValue 根据字段类型设置值
func setFieldValue(field reflect.Value, value string, fieldName string) (bool, error) {
	switch field.Kind() {
	case reflect.String:
		oldValue := field.String()
		field.SetString(value)
		return true, fmt.Errorf("  %s: '%s' -> '%s'", fieldName, oldValue, value)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		intVal, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return false, fmt.Errorf("%s 需要整数类型，收到: %s", fieldName, value)
		}
		oldValue := field.Int()
		field.SetInt(intVal)
		return true, fmt.Errorf("  %s: %d -> %d", fieldName, oldValue, intVal)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		uintVal, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return false, fmt.Errorf("%s 需要无符号整数类型，收到: %s", fieldName, value)
		}
		oldValue := field.Uint()
		field.SetUint(uintVal)
		return true, fmt.Errorf("  %s: %d -> %d", fieldName, oldValue, uintVal)

	case reflect.Float32, reflect.Float64:
		floatVal, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return false, fmt.Errorf("%s 需要浮点数类型，收到: %s", fieldName, value)
		}
		oldValue := field.Float()
		field.SetFloat(floatVal)
		return true, fmt.Errorf("  %s: %f -> %f", fieldName, oldValue, floatVal)

	case reflect.Bool:
		boolVal, err := strconv.ParseBool(value)
		if err != nil {
			return false, fmt.Errorf("%s 需要布尔类型 (true/false)，收到: %s", fieldName, value)
		}
		oldValue := field.Bool()
		field.SetBool(boolVal)
		return true, fmt.Errorf("  %s: %t -> %t", fieldName, oldValue, boolVal)

	default:
		return false, fmt.Errorf("不支持的字段类型: %s", field.Kind())
	}
}

// setConfigField 使用缓存
func (qs *QuantSystem) setConfigField(fieldName string, value string) (bool, error) {
	normalized := strings.ToLower(strings.ReplaceAll(fieldName, "_", ""))

	info, exists := qs.configFields[normalized]
	if !exists {
		return false, fmt.Errorf("未知配置项: %s", fieldName)
	}

	configValue := reflect.ValueOf(qs.AppConfig).Elem()
	fieldValue := configValue.Field(info.FieldIndex)

	if !fieldValue.CanSet() {
		return false, fmt.Errorf("字段 %s 不可设置", info.FieldName)
	}

	return setFieldValue(fieldValue, value, info.FieldName)
}

// getConfigField 使用缓存获取配置字段的值
func (qs *QuantSystem) getConfigField(fieldName string) (interface{}, error) {
	normalized := strings.ToLower(strings.ReplaceAll(fieldName, "_", ""))

	info, exists := qs.configFields[normalized]
	if !exists {
		return nil, fmt.Errorf("未知配置项: %s", fieldName)
	}

	configValue := reflect.ValueOf(qs.AppConfig).Elem()
	fieldValue := configValue.Field(info.FieldIndex)

	// Get 操作只需要检查是否可访问，不需要 CanSet()
	if !fieldValue.IsValid() {
		return nil, fmt.Errorf("字段 %s 无效", info.FieldName)
	}

	// 返回字段的实际值
	return fieldValue.Interface(), nil
}

// GetAllFlagConfigField 获取所有标识字段
func (qs *QuantSystem) GetAllFlagConfigField(flag string) map[string]string {
	configValue := reflect.ValueOf(qs.AppConfig).Elem()
	configType := configValue.Type()

	data := make(map[string]string)

	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		fieldValue := configValue.Field(i)

		// 跳过私有字段
		if field.PkgPath != "" {
			continue
		}

		if field.Tag.Get("flag") != flag {
			continue
		}

		// 格式化显示
		fieldName := field.Name
		var currentValue string

		switch fieldValue.Kind() {
		case reflect.String:
			currentValue = fieldValue.String()
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			currentValue = strconv.FormatInt(fieldValue.Int(), 10)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			currentValue = strconv.FormatUint(fieldValue.Uint(), 10)
		case reflect.Float32, reflect.Float64:
			currentValue = strconv.FormatFloat(fieldValue.Float(), 'f', -1, 64)
		case reflect.Bool:
			currentValue = strconv.FormatBool(fieldValue.Bool())
		default:
			continue
		}
		data[fieldName] = currentValue
	}

	return data
}

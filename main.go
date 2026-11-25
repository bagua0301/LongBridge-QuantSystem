package main

import (
	"LongBridge-QuantSystem/logger"
	"LongBridge-QuantSystem/quant"
)

// main 程序入口
func main() {
	system, err := quant.NewQuantSystem()
	if err != nil {
		logger.Error("初始化失败: %v", err)
		panic(err)
	}

	system.Run()

	// 关闭所有日志器
	logger.CloseLogger()
}

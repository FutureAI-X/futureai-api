package common

import (
	"fmt"
	"log"
	"os"
)

// DebugEnabled 是否启用调试模式
var DebugEnabled = false

// ApplyDebugSetting 依据 DEBUG 环境变量设置调试开关。
//
// 必须在 godotenv.Load() 之后、model.InitDB() 之前调用：
// 前者保证 .env 里的值已进入环境，后者会读取本开关决定是否打印 SQL 日志。
// 顺序错了这个配置就永远不生效（此前 DebugEnabled 是硬编码的 false，
// DEBUG 变量在任何地方都没有被读取，却出现在文档和上线自检清单里）。
func ApplyDebugSetting() {
	DebugEnabled = GetEnvOrDefaultBool("DEBUG", false)
	if DebugEnabled {
		SysLog("[警告] DEBUG 已开启：将输出 SQL 日志，生产环境请务必关闭")
	}
}

// SysLog 系统日志
func SysLog(msg string) {
	log.Printf("[FUTUREAI-API] %s", msg)
}

// SysError 系统错误日志
func SysError(msg string) {
	log.Printf("[FUTUREAI-API] [ERROR] %s", msg)
}

// FatalLog 致命错误日志
func FatalLog(msg string) {
	log.Fatalf("[FUTUREAI-API] [FATAL] %s", msg)
	os.Exit(1)
}

// SysLogf 格式化系统日志
func SysLogf(format string, args ...interface{}) {
	SysLog(fmt.Sprintf(format, args...))
}

// SysErrorf 格式化系统错误日志
func SysErrorf(format string, args ...interface{}) {
	SysError(fmt.Sprintf(format, args...))
}

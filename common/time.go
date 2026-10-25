package common

import "time"

// DisplayTimeLayout 是接口返回给前端的时间格式。
// 带空格的这种格式前端用 new Date() 解析时按本地时间处理，
// 因此输出的必须是北京时间的墙上时间，而不是 UTC。
const DisplayTimeLayout = "2006-01-02 15:04:05"

// displayLocation 是展示用时间的时区。
//
// 数据库里是 timestamptz，存的是绝对时刻（正确），但驱动读回来时
// time.Time 的 Location 是 UTC。直接 Format 会得到比北京时间早 8 小时的
// 字符串（如 14:25 显示成 06:25）。
//
// 刻意固定 Asia/Shanghai 而不是用 time.Local：显示时区不应随部署机器的
// TZ 变化，否则换台服务器或容器没设 TZ，页面上同一个时间就会漂移。
// LoadLocation 依赖 tzdata，镜像里没有时退回固定 +08:00。
var displayLocation = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*60*60)
}()

// FormatDisplayTime 把数据库读出的时间转成北京时间字符串。
func FormatDisplayTime(t time.Time) string {
	return t.In(displayLocation).Format(DisplayTimeLayout)
}

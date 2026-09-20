package middleware

import (
	"net/http"

	"github.com/FutureAI-X/futureai-api/common"
	"github.com/gin-gonic/gin"
)

// BodyLimit 限制请求体大小，超限返回 413。
//
// 两道防线，缺一不可：
//
//  1. Content-Length 预检——超限直接拒绝，请求体一个字节都不会被读。
//     这挡住了绝大多数情况（任何正常 HTTP 客户端/攻击脚本都会带这个头），
//     也让超大请求在最早的时刻就被拒掉。
//
//  2. MaxBytesReader 包裹——针对分块传输（Transfer-Encoding: chunked）
//     或谎报 Content-Length 的请求，在**读取过程中**拦截。少了它，
//     这类请求仍能把内存打满。
//
// 注意 goroutine 无法预先读取 body 后回写，所以第二道防线触发的错误会
// 由具体处理函数在读取时才拿到；各处用 common.IsBodyTooLarge 判定并返回 413。
func BodyLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := common.BodyLimitFor(c.Request.URL.Path)

		if c.Request.ContentLength > limit {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"code":    "fail",
				"message": "请求体过大",
			})
			return
		}

		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}

		c.Next()
	}
}

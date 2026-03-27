package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Context key 常量
const (
	ContextKeyUserID    = "user_id"
	ContextKeyUserLogin = "user_login"
	ContextKeyUserName  = "user_name"
	ContextKeyAvatar    = "user_avatar"
)

// Middleware 认证中间件
// 如果 auth 未启用，所有请求视为匿名用户（user_id = "anonymous"）
// 对 /auth/* 路径跳过认证
func Middleware(mgr *Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		// 跳过认证的路径
		if strings.HasPrefix(path, "/auth/") ||
			path == "/health" ||
			strings.HasPrefix(path, "/assets/") ||
			path == "/" ||
			strings.HasSuffix(path, ".js") ||
			strings.HasSuffix(path, ".css") ||
			strings.HasSuffix(path, ".html") ||
			strings.HasSuffix(path, ".ico") {
			c.Set(ContextKeyUserID, "anonymous")
			c.Next()
			return
		}

		// 认证未启用 → 匿名模式
		if !mgr.IsEnabled() {
			c.Set(ContextKeyUserID, "anonymous")
			c.Next()
			return
		}

		// 从 Authorization header 或 query param 获取 token
		tokenStr := c.GetHeader("Authorization")
		if tokenStr == "" {
			tokenStr = c.Query("token")
		}
		if tokenStr == "" {
			// MCP SSE 连接可能没有 Authorization header，用 session 兜底
			c.Set(ContextKeyUserID, "anonymous")
			c.Next()
			return
		}

		claims, err := mgr.ValidateJWT(tokenStr)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "认证失败: " + err.Error()})
			c.Abort()
			return
		}

		c.Set(ContextKeyUserID, claims.UserID)
		c.Set(ContextKeyUserLogin, claims.Login)
		c.Set(ContextKeyUserName, claims.Name)
		c.Set(ContextKeyAvatar, claims.AvatarURL)
		c.Next()
	}
}

// GetUserID 从 gin.Context 获取用户 ID
func GetUserID(c *gin.Context) string {
	if id, ok := c.Get(ContextKeyUserID); ok {
		return id.(string)
	}
	return "anonymous"
}

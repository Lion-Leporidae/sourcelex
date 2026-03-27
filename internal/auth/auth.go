// Package auth 提供 GitHub OAuth2 认证和 JWT token 管理
package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Config 认证配置
type Config struct {
	// GitHub OAuth App 配置
	GitHubClientID     string `mapstructure:"github_client_id"`
	GitHubClientSecret string `mapstructure:"github_client_secret"`

	// JWT 签名密钥
	JWTSecret string `mapstructure:"jwt_secret"`

	// JWT 过期时间（小时）
	JWTExpireHours int `mapstructure:"jwt_expire_hours"`

	// 是否启用认证（false = 跳过认证，所有请求视为匿名）
	Enabled bool `mapstructure:"enabled"`
}

// UserInfo GitHub 用户信息
type UserInfo struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	Email     string `json:"email"`
}

// UserClaims JWT claims
type UserClaims struct {
	UserID    string `json:"user_id"`    // "github:<id>"
	Login     string `json:"login"`      // GitHub username
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	jwt.RegisteredClaims
}

// Manager 认证管理器
type Manager struct {
	cfg Config
}

// NewManager 创建认证管理器
func NewManager(cfg Config) *Manager {
	if cfg.JWTExpireHours <= 0 {
		cfg.JWTExpireHours = 168 // 默认 7 天
	}
	if cfg.JWTSecret == "" {
		cfg.JWTSecret = "sourcelex-default-secret-change-me"
	}
	return &Manager{cfg: cfg}
}

// IsEnabled 认证是否启用
func (m *Manager) IsEnabled() bool {
	return m.cfg.Enabled && m.cfg.GitHubClientID != "" && m.cfg.GitHubClientSecret != ""
}

// GetAuthURL 生成 GitHub OAuth 授权 URL
func (m *Manager) GetAuthURL(redirectURI string) string {
	params := url.Values{
		"client_id":    {m.cfg.GitHubClientID},
		"redirect_uri": {redirectURI},
		"scope":        {"read:user user:email"},
	}
	return "https://github.com/login/oauth/authorize?" + params.Encode()
}

// ExchangeCode 用授权码交换 access_token，然后获取用户信息
func (m *Manager) ExchangeCode(code, redirectURI string) (*UserInfo, error) {
	// 1. 交换 access_token
	tokenResp, err := http.PostForm("https://github.com/login/oauth/access_token", url.Values{
		"client_id":     {m.cfg.GitHubClientID},
		"client_secret": {m.cfg.GitHubClientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	})
	if err != nil {
		return nil, fmt.Errorf("交换 token 失败: %w", err)
	}
	defer tokenResp.Body.Close()

	body, _ := io.ReadAll(tokenResp.Body)
	vals, _ := url.ParseQuery(string(body))
	accessToken := vals.Get("access_token")
	if accessToken == "" {
		// GitHub 有时返回 JSON
		var jsonResp struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
		}
		if json.Unmarshal(body, &jsonResp) == nil && jsonResp.AccessToken != "" {
			accessToken = jsonResp.AccessToken
		} else {
			errMsg := vals.Get("error_description")
			if errMsg == "" {
				errMsg = string(body)
			}
			return nil, fmt.Errorf("获取 access_token 失败: %s", errMsg)
		}
	}

	// 2. 获取用户信息
	return m.fetchGitHubUser(accessToken)
}

// fetchGitHubUser 通过 access_token 获取 GitHub 用户信息
func (m *Manager) fetchGitHubUser(accessToken string) (*UserInfo, error) {
	req, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("获取用户信息失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API 错误 %d: %s", resp.StatusCode, string(body))
	}

	var user UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("解析用户信息失败: %w", err)
	}
	return &user, nil
}

// IssueJWT 签发 JWT
func (m *Manager) IssueJWT(user *UserInfo) (string, error) {
	claims := UserClaims{
		UserID:    fmt.Sprintf("github:%d", user.ID),
		Login:     user.Login,
		Name:      user.Name,
		AvatarURL: user.AvatarURL,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(m.cfg.JWTExpireHours) * time.Hour)),
			Issuer:    "sourcelex",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(m.cfg.JWTSecret))
}

// ValidateJWT 验证 JWT 并返回 claims
func (m *Manager) ValidateJWT(tokenStr string) (*UserClaims, error) {
	// 去掉 Bearer 前缀
	tokenStr = strings.TrimPrefix(tokenStr, "Bearer ")
	tokenStr = strings.TrimSpace(tokenStr)

	token, err := jwt.ParseWithClaims(tokenStr, &UserClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("不支持的签名方法: %v", token.Header["alg"])
		}
		return []byte(m.cfg.JWTSecret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("JWT 验证失败: %w", err)
	}

	claims, ok := token.Claims.(*UserClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("无效的 JWT token")
	}
	return claims, nil
}

package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Claims 族谱系统签发的短时身份（SSO）
type Claims struct {
	Sub    int64  `json:"sub"`    // 族谱用户 ID
	Name   string `json:"name"`   // 用户名
	Role   string `json:"role"`   // 角色（族员/理事/...）
	Tenant string `json:"tenant"` // 家族空间（预留多租户）
	Surname string `json:"surname,omitempty"`
	Exp    int64  `json:"exp"`
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func hmacPart(secret, data string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(data))
	return b64(h.Sum(nil))
}

// Sign 生成 HS256 JWT（族谱侧与自测用）
func Sign(secret string, c Claims) string {
	header := b64([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, _ := json.Marshal(c)
	body := header + "." + b64(payload)
	return body + "." + hmacPart(secret, body)
}

// Verify 校验 HS256 JWT
func Verify(secret, token string) (*Claims, error) {
	if secret == "" {
		return nil, errors.New("未配置密钥")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("令牌格式错误")
	}
	body := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(hmacPart(secret, body)), []byte(parts[2])) {
		return nil, errors.New("签名校验失败")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var c Claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	if c.Exp > 0 && time.Now().Unix() > c.Exp {
		return nil, errors.New("令牌已过期")
	}
	return &c, nil
}

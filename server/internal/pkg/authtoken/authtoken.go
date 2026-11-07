package authtoken

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// 令牌用途域：App 令牌不能访问管理后台接口。
const (
	ScopeApp   = "app"
	ScopeAdmin = "admin"
)

var ErrInvalid = errors.New("令牌无效或已过期")

type Claims struct {
	UserID string `json:"uid"`
	Scope  string `json:"scope"`
	jwt.RegisteredClaims
}

func Sign(secret, userID, scope string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: userID,
		Scope:  scope,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "family-ledger",
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func Parse(secret, token, wantScope string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalid
		}
		return []byte(secret), nil
	}, jwt.WithIssuer("family-ledger"))
	if err != nil {
		return nil, ErrInvalid
	}
	if wantScope != "" && claims.Scope != wantScope {
		return nil, ErrInvalid
	}
	return claims, nil
}

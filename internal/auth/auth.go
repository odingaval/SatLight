package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/satlight/satlight/internal/config"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrInvalidToken = errors.New("invalid token")
)

type Service struct {
	cfg  *config.Config
	pool *pgxpool.Pool
	log  *zerolog.Logger
}

type Claims struct {
	UserID int64 `json:"user_id"`
	jwt.RegisteredClaims
}

func NewService(cfg *config.Config, pool *pgxpool.Pool) *Service {
	return &Service{
		cfg:  cfg,
		pool: pool,
		log:  zerolog.New(zerolog.ConsoleWriter{Out: zerolog.ConsoleWriter{Out: zerolog.ConsoleWriter{Out: gin.DefaultWriter, TimeFormat: zerolog.TimeFormatUnix}}}).With().Timestamp().Logger(),
	}
}

func (s *Service) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionCookie, err := c.Cookie("satlight_session")
		if err != nil {
			s.unauthorized(c, "missing session cookie")
			return
		}

		claims, err := s.ValidateToken(sessionCookie)
		if err != nil {
			s.unauthorized(c, "invalid session")
			return
		}

		c.Set("user_id", claims.UserID)
		c.Next()
	}
}

func (s *Service) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return []byte(s.cfg.SessionSecret), nil
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

func (s *Service) GenerateToken(userID int64) (string, error) {
	claims := &Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(30 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.cfg.SessionSecret))
}

func (s *Service) SetSessionCookie(c *gin.Context, token string) {
	c.SetCookie("satlight_session", token, 30*24*3600, "/", "", false, true)
}

func (s *Service) ClearSessionCookie(c *gin.Context) {
	c.SetCookie("satlight_session", "", -1, "/", "", false, true)
}

func (s *Service) unauthorized(c *gin.Context, msg string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHORIZED", "message": msg}})
}

func VerifyGitHubSignature(payload []byte, signature, secret string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	sig := strings.TrimPrefix(signature, "sha256=")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

func GetUserID(c *gin.Context) (int64, bool) {
	userID, exists := c.Get("user_id")
	if !exists {
		return 0, false
	}
	id, ok := userID.(int64)
	return id, ok
}

func ContextWithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, userIDKey{}, userID)
}

func UserIDFromContext(ctx context.Context) (int64, bool) {
	v := ctx.Value(userIDKey{})
	if v == nil {
		return 0, false
	}
	id, ok := v.(int64)
	return id, ok
}

type userIDKey struct{}
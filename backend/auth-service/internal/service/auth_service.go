package service

import (
    "crypto/rand"
    "encoding/hex"
    "errors"
    "time"

    "github.com/golang-jwt/jwt/v5"
    "golang.org/x/crypto/bcrypt"
    "github.com/uber-clone/auth-service/internal/model"
    "github.com/uber-clone/auth-service/internal/repository"
)

type AuthService struct {
    repo      *repository.AuthRepository
    jwtSecret []byte
}

func NewAuthService(repo *repository.AuthRepository, jwtSecret []byte) *AuthService {
    return &AuthService{repo: repo, jwtSecret: jwtSecret}
}

func (s *AuthService) Register(email, phone, password, role string) (*model.User, string, string, error) {
    existing, _ := s.repo.GetUserByEmail(email)
    if existing != nil {
        return nil, "", "", errors.New("user already exists")
    }

    hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
    if err != nil {
        return nil, "", "", err
    }

    user := &model.User{
        ID:        generateUUID(),
        Email:     email,
        Phone:     phone,
        Password:  string(hashed),
        Role:      role,
        CreatedAt: time.Now(),
        UpdatedAt: time.Now(),
    }

    if err := s.repo.CreateUser(user); err != nil {
        return nil, "", "", err
    }

    accessToken, refreshToken, err := s.generateTokens(user.ID, user.Role)
    if err != nil {
        return nil, "", "", err
    }

    s.repo.UpdateRefreshToken(user.ID, refreshToken)
    return user, accessToken, refreshToken, nil
}

func (s *AuthService) Login(email, password string) (*model.User, string, string, error) {
    user, err := s.repo.GetUserByEmail(email)
    if err != nil || user == nil {
        return nil, "", "", errors.New("invalid credentials")
    }

    if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
        return nil, "", "", errors.New("invalid credentials")
    }

    if !user.IsActive {
        return nil, "", "", errors.New("account deactivated")
    }

    accessToken, refreshToken, err := s.generateTokens(user.ID, user.Role)
    if err != nil {
        return nil, "", "", err
    }

    s.repo.UpdateRefreshToken(user.ID, refreshToken)
    s.repo.UpdateLastLogin(user.ID)
    return user, accessToken, refreshToken, nil
}

func (s *AuthService) ValidateToken(tokenString string) (string, string, bool) {
    token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
        return s.jwtSecret, nil
    })
    if err != nil || !token.Valid {
        return "", "", false
    }
    claims, ok := token.Claims.(jwt.MapClaims)
    if !ok {
        return "", "", false
    }
    return claims["user_id"].(string), claims["role"].(string), true
}

func (s *AuthService) RefreshToken(refreshToken string) (string, string, error) {
    token, err := jwt.Parse(refreshToken, func(t *jwt.Token) (interface{}, error) {
        return s.jwtSecret, nil
    })
    if err != nil || !token.Valid {
        return "", "", errors.New("invalid refresh token")
    }
    claims, ok := token.Claims.(jwt.MapClaims)
    if !ok {
        return "", "", errors.New("invalid token claims")
    }
    userID := claims["user_id"].(string)
    role := claims["role"].(string)

    var user model.User
    if err := s.repo.GetUserByID(userID); err != nil {
        return "", "", errors.New("user not found")
    }

    accessToken, newRefreshToken, err := s.generateTokens(userID, role)
    if err != nil {
        return "", "", err
    }
    s.repo.UpdateRefreshToken(userID, newRefreshToken)
    return accessToken, newRefreshToken, nil
}

func (s *AuthService) Logout(refreshToken string) error {
    token, err := jwt.Parse(refreshToken, func(t *jwt.Token) (interface{}, error) {
        return s.jwtSecret, nil
    })
    if err != nil {
        return err
    }
    claims := token.Claims.(jwt.MapClaims)
    userID := claims["user_id"].(string)
    return s.repo.UpdateRefreshToken(userID, "")
}

func (s *AuthService) generateTokens(userID, role string) (string, string, error) {
    accessClaims := jwt.MapClaims{
        "user_id": userID,
        "role":    role,
        "exp":     time.Now().Add(15 * time.Minute).Unix(),
        "iat":     time.Now().Unix(),
        "jti":     generateJTI(),
    }
    accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
    accessString, err := accessToken.SignedString(s.jwtSecret)
    if err != nil {
        return "", "", err
    }

    refreshClaims := jwt.MapClaims{
        "user_id": userID,
        "exp":     time.Now().Add(7 * 24 * time.Hour).Unix(),
        "jti":     generateJTI(),
    }
    refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
    refreshString, err := refreshToken.SignedString(s.jwtSecret)
    return accessString, refreshString, err
}

func generateUUID() string {
    return "user_" + time.Now().Format("20060102150405") + "_" + randomString(6)
}

func generateJTI() string {
    b := make([]byte, 16)
    rand.Read(b)
    return hex.EncodeToString(b)
}

func randomString(n int) string {
    const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
    b := make([]byte, n)
    for i := range b {
        b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
    }
    return string(b)
}
package service

import (
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

func (s *AuthService) Register(email, phone, password, role string) (*model.User, string, error) {
    existing, _ := s.repo.GetUserByEmail(email)
    if existing != nil {
        return nil, "", errors.New("user already exists")
    }

    hashed, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
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
        return nil, "", err
    }

    token, err := s.generateJWT(user.ID, user.Role)
    return user, token, err
}

func (s *AuthService) Login(email, password string) (*model.User, string, error) {
    user, err := s.repo.GetUserByEmail(email)
    if err != nil || user == nil {
        return nil, "", errors.New("invalid credentials")
    }

    if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
        return nil, "", errors.New("invalid credentials")
    }

    token, err := s.generateJWT(user.ID, user.Role)
    return user, token, err
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

func (s *AuthService) generateJWT(userID, role string) (string, error) {
    claims := jwt.MapClaims{
        "user_id": userID,
        "role":    role,
        "exp":     time.Now().Add(24 * time.Hour).Unix(),
    }
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    return token.SignedString(s.jwtSecret)
}

func generateUUID() string {
    return "user_" + time.Now().Format("20060102150405")
}
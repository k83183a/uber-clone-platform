package handler

import (
    "context"

    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    pb "github.com/uber-clone/auth-service/proto"
    "github.com/uber-clone/auth-service/internal/service"
)

type GrpcHandler struct {
    pb.UnimplementedAuthServiceServer
    svc *service.AuthService
}

func NewGrpcHandler(svc *service.AuthService) *GrpcHandler {
    return &GrpcHandler{svc: svc}
}

func (h *GrpcHandler) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.AuthResponse, error) {
    user, token, err := h.svc.Register(req.Email, req.Phone, req.Password, req.Role)
    if err != nil {
        return nil, status.Error(codes.AlreadyExists, err.Error())
    }
    return &pb.AuthResponse{
        Token:   token,
        UserId:  user.ID,
        Email:   user.Email,
        Role:    user.Role,
        ExpiresIn: 86400,
    }, nil
}

func (h *GrpcHandler) Login(ctx context.Context, req *pb.LoginRequest) (*pb.AuthResponse, error) {
    user, token, err := h.svc.Login(req.Email, req.Password)
    if err != nil {
        return nil, status.Error(codes.Unauthenticated, err.Error())
    }
    return &pb.AuthResponse{
        Token:   token,
        UserId:  user.ID,
        Email:   user.Email,
        Role:    user.Role,
        ExpiresIn: 86400,
    }, nil
}

func (h *GrpcHandler) ValidateToken(ctx context.Context, req *pb.ValidateTokenRequest) (*pb.ValidateTokenResponse, error) {
    userID, role, valid := h.svc.ValidateToken(req.AccessToken)
    return &pb.ValidateTokenResponse{
        Valid:  valid,
        UserId: userID,
        Role:   role,
    }, nil
}
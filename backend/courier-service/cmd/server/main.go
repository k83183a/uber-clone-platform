package main

import (
    "context"
    "log"
    "net"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/joho/godotenv"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"

    pb "github.com/uber-clone/courier-service/proto"
)

// ============================================================
// MODELS
// ============================================================

type Parcel struct {
    ID                   string     `gorm:"primaryKey"`
    TrackingNumber       string     `gorm:"uniqueIndex;not null"`
    SenderID             string     `gorm:"index;not null"`
    SenderName           string     `gorm:"not null"`
    SenderPhone          string     `gorm:"not null"`
    SenderAddress        string     `gorm:"not null"`
    SenderLat            float64
    SenderLng            float64
    RecipientName        string     `gorm:"not null"`
    RecipientPhone       string     `gorm:"not null"`
    RecipientAddress     string     `gorm:"not null"`
    RecipientLat         float64
    RecipientLng         float64
    PackageType          string     `gorm:"not null"` // document, box, envelope, fragile, large
    WeightKg             float64
    Dimensions           string     // LxWxH in cm
    SpecialInstructions  string
    Status               string     `gorm:"default:'pending'"`
    Fee                  float64    `gorm:"not null"`
    PaymentMethod        string
    CourierID            string     `gorm:"index"`
    EstimatedMinutes     int
    ActualMinutes        int
    CreatedAt            time.Time
    AssignedAt           *time.Time
    PickedUpAt           *time.Time
    InTransitAt          *time.Time
    OutForDeliveryAt     *time.Time
    DeliveredAt          *time.Time
    CancelledAt          *time.Time
    CancelledReason      string
    ProofImageURL        string
    SignatureURL         string
}

type DeliveryProof struct {
    ID          string    `gorm:"primaryKey"`
    ParcelID    string    `gorm:"index;not null"`
    ProofType   string    // photo, signature
    ImageURL    string
    CapturedAt  time.Time
}

type CourierServer struct {
    pb.UnimplementedCourierServiceServer
    DB *gorm.DB
}

// CreateParcel - Create a new parcel delivery request
func (s *CourierServer) CreateParcel(ctx context.Context, req *pb.CreateParcelRequest) (*pb.ParcelResponse, error) {
    fee := calculateFee(req)

    parcel := &Parcel{
        ID:                  generateID(),
        TrackingNumber:      generateTrackingNumber(),
        SenderID:            req.SenderId,
        SenderName:          req.SenderName,
        SenderPhone:         req.SenderPhone,
        SenderAddress:       req.SenderAddress,
        SenderLat:           req.SenderLat,
        SenderLng:           req.SenderLng,
        RecipientName:       req.RecipientName,
        RecipientPhone:      req.RecipientPhone,
        RecipientAddress:    req.RecipientAddress,
        RecipientLat:        req.RecipientLat,
        RecipientLng:        req.RecipientLng,
        PackageType:         req.PackageType,
        WeightKg:            req.WeightKg,
        Dimensions:          req.Dimensions,
        SpecialInstructions: req.SpecialInstructions,
        Status:              "pending",
        Fee:                 fee,
        PaymentMethod:       req.PaymentMethod,
        EstimatedMinutes:    60, // 1 hour estimated
        CreatedAt:           time.Now(),
    }

    if err := s.DB.Create(parcel).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create parcel")
    }

    // In production: push to dispatch service for courier assignment
    log.Printf("📦 New parcel %s created. Tracking: %s", parcel.ID, parcel.TrackingNumber)

    return &pb.ParcelResponse{
        Id:             parcel.ID,
        TrackingNumber: parcel.TrackingNumber,
        Status:         parcel.Status,
        Fee:            parcel.Fee,
        EstimatedMinutes: int32(parcel.EstimatedMinutes),
        CreatedAt:      parcel.CreatedAt.Unix(),
    }, nil
}

// GetParcel - Get parcel details
func (s *CourierServer) GetParcel(ctx context.Context, req *pb.GetParcelRequest) (*pb.ParcelDetailResponse, error) {
    var parcel Parcel
    if err := s.DB.Where("id = ? OR tracking_number = ?", req.ParcelId, req.TrackingNumber).First(&parcel).Error; err != nil {
        return nil, status.Error(codes.NotFound, "parcel not found")
    }

    return &pb.ParcelDetailResponse{
        Id:             parcel.ID,
        TrackingNumber: parcel.TrackingNumber,
        SenderName:     parcel.SenderName,
        SenderPhone:    parcel.SenderPhone,
        SenderAddress:  parcel.SenderAddress,
        RecipientName:  parcel.RecipientName,
        RecipientPhone: parcel.RecipientPhone,
        RecipientAddress: parcel.RecipientAddress,
        PackageType:    parcel.PackageType,
        WeightKg:       parcel.WeightKg,
        Dimensions:     parcel.Dimensions,
        Status:         parcel.Status,
        Fee:            parcel.Fee,
        CourierId:      parcel.CourierID,
        EstimatedMinutes: int32(parcel.EstimatedMinutes),
        CreatedAt:      parcel.CreatedAt.Unix(),
    }, nil
}

// UpdateParcelStatus - Update parcel status (courier side)
func (s *CourierServer) UpdateParcelStatus(ctx context.Context, req *pb.UpdateParcelStatusRequest) (*pb.Empty, error) {
    var parcel Parcel
    if err := s.DB.Where("id = ?", req.ParcelId).First(&parcel).Error; err != nil {
        return nil, status.Error(codes.NotFound, "parcel not found")
    }

    now := time.Now()
    updates := map[string]interface{}{"status": req.Status}

    switch req.Status {
    case "assigned":
        updates["assigned_at"] = now
        updates["courier_id"] = req.CourierId
    case "picked_up":
        updates["picked_up_at"] = now
    case "in_transit":
        updates["in_transit_at"] = now
    case "out_for_delivery":
        updates["out_for_delivery_at"] = now
    case "delivered":
        updates["delivered_at"] = now
        actualTime := int(now.Sub(parcel.CreatedAt).Minutes())
        updates["actual_minutes"] = actualTime
    case "cancelled":
        updates["cancelled_at"] = now
        updates["cancelled_reason"] = req.Reason
    }

    if err := s.DB.Model(&parcel).Updates(updates).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to update status")
    }

    return &pb.Empty{}, nil
}

// AddDeliveryProof - Add proof of delivery (photo/signature)
func (s *CourierServer) AddDeliveryProof(ctx context.Context, req *pb.AddDeliveryProofRequest) (*pb.Empty, error) {
    var parcel Parcel
    if err := s.DB.Where("id = ?", req.ParcelId).First(&parcel).Error; err != nil {
        return nil, status.Error(codes.NotFound, "parcel not found")
    }

    proof := &DeliveryProof{
        ID:         generateID(),
        ParcelID:   req.ParcelId,
        ProofType:  req.ProofType,
        ImageURL:   req.ImageUrl,
        CapturedAt: time.Now(),
    }
    s.DB.Create(proof)

    if req.ProofType == "photo" {
        parcel.ProofImageURL = req.ImageUrl
    } else if req.ProofType == "signature" {
        parcel.SignatureURL = req.ImageUrl
    }
    s.DB.Save(&parcel)

    return &pb.Empty{}, nil
}

// ListUserParcels - List parcels for a user (sender)
func (s *CourierServer) ListUserParcels(ctx context.Context, req *pb.ListUserParcelsRequest) (*pb.ListParcelsResponse, error) {
    var parcels []Parcel
    query := s.DB.Where("sender_id = ?", req.UserId).Order("created_at DESC")

    offset := (req.Page - 1) * req.PageSize
    if err := query.Offset(int(offset)).Limit(int(req.PageSize)).Find(&parcels).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list parcels")
    }

    var total int64
    s.DB.Model(&Parcel{}).Where("sender_id = ?", req.UserId).Count(&total)

    var pbParcels []*pb.ParcelSummary
    for _, p := range parcels {
        pbParc
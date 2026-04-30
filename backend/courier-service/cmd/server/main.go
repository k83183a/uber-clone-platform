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

// Parcel represents a delivery parcel
type Parcel struct {
    ID               string     `gorm:"primaryKey"`
    TrackingNumber   string     `gorm:"uniqueIndex;not null"`
    SenderID         string     `gorm:"index;not null"`
    SenderName       string     `gorm:"not null"`
    SenderPhone      string     `gorm:"not null"`
    SenderAddress    string     `gorm:"not null"`
    SenderLat        float64
    SenderLng        float64
    RecipientName    string     `gorm:"not null"`
    RecipientPhone   string     `gorm:"not null"`
    RecipientAddress string     `gorm:"not null"`
    RecipientLat     float64
    RecipientLng     float64
    PackageType      string     `gorm:"not null"` // document, box, envelope, fragile, large
    WeightKg         float64
    Dimensions       string     // LxWxH in cm
    SpecialInstructions string
    Status           string     `gorm:"default:'pending'"` // pending, picked_up, in_transit, out_for_delivery, delivered, cancelled
    Fee              float64    `gorm:"not null"`
    PaymentMethod    string
    CourierID        string     `gorm:"index"`
    CreatedAt        time.Time
    PickedUpAt       *time.Time
    DeliveredAt      *time.Time
    CancelledAt      *time.Time
    CancelledReason  string
    ProofImageURL    string
    SignatureURL     string
}

// DeliveryProof stores proof of delivery (photo/signature)
type DeliveryProof struct {
    ID          string    `gorm:"primaryKey"`
    ParcelID    string    `gorm:"index;not null"`
    ProofType   string    // photo, signature
    ImageURL    string
    CapturedAt  time.Time
}

// CourierServer handles gRPC requests
type CourierServer struct {
    pb.UnimplementedCourierServiceServer
    DB *gorm.DB
}

// CreateParcel creates a new parcel delivery request
func (s *CourierServer) CreateParcel(ctx context.Context, req *pb.CreateParcelRequest) (*pb.ParcelResponse, error) {
    // Calculate delivery fee based on distance, weight, package type
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
        CreatedAt:           time.Now(),
    }

    if err := s.DB.Create(parcel).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create parcel")
    }

    return &pb.ParcelResponse{
        Id:             parcel.ID,
        TrackingNumber: parcel.TrackingNumber,
        Status:         parcel.Status,
        Fee:            parcel.Fee,
        CreatedAt:      parcel.CreatedAt.String(),
    }, nil
}

// GetParcel returns parcel details
func (s *CourierServer) GetParcel(ctx context.Context, req *pb.GetParcelRequest) (*pb.ParcelDetailResponse, error) {
    var parcel Parcel
    if err := s.DB.Where("id = ? OR tracking_number = ?", req.Id, req.TrackingNumber).First(&parcel).Error; err != nil {
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
        CreatedAt:      parcel.CreatedAt.String(),
    }, nil
}

// UpdateParcelStatus updates parcel status (picked_up, in_transit, delivered)
func (s *CourierServer) UpdateParcelStatus(ctx context.Context, req *pb.UpdateParcelStatusRequest) (*pb.Empty, error) {
    var parcel Parcel
    if err := s.DB.Where("id = ?", req.ParcelId).First(&parcel).Error; err != nil {
        return nil, status.Error(codes.NotFound, "parcel not found")
    }

    updates := map[string]interface{}{
        "status":     req.Status,
    }

    now := time.Now()
    if req.Status == "picked_up" {
        updates["picked_up_at"] = now
    } else if req.Status == "delivered" {
        updates["delivered_at"] = now
    }

    if req.CourierId != "" {
        updates["courier_id"] = req.CourierId
    }

    if err := s.DB.Model(&parcel).Updates(updates).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to update status")
    }

    return &pb.Empty{}, nil
}

// CancelParcel cancels a parcel delivery
func (s *CourierServer) CancelParcel(ctx context.Context, req *pb.CancelParcelRequest) (*pb.Empty, error) {
    var parcel Parcel
    if err := s.DB.Where("id = ?", req.ParcelId).First(&parcel).Error; err != nil {
        return nil, status.Error(codes.NotFound, "parcel not found")
    }

    if parcel.Status != "pending" && parcel.Status != "picked_up" {
        return nil, status.Error(codes.FailedPrecondition, "parcel cannot be cancelled")
    }

    now := time.Now()
    parcel.Status = "cancelled"
    parcel.CancelledAt = &now
    parcel.CancelledReason = req.Reason

    if err := s.DB.Save(&parcel).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to cancel parcel")
    }

    return &pb.Empty{}, nil
}

// AddDeliveryProof adds proof of delivery (photo/signature)
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

    if err := s.DB.Create(proof).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to add delivery proof")
    }

    // Update parcel with proof URL
    if req.ProofType == "photo" {
        parcel.ProofImageURL = req.ImageUrl
    } else if req.ProofType == "signature" {
        parcel.SignatureURL = req.ImageUrl
    }
    s.DB.Save(&parcel)

    return &pb.Empty{}, nil
}

// ListUserParcels lists parcels for a user (as sender)
func (s *CourierServer) ListUserParcels(ctx context.Context, req *pb.ListUserParcelsRequest) (*pb.ListParcelsResponse, error) {
    var parcels []Parcel
    query := s.DB.Where("sender_id = ?", req.UserId).Order("created_at DESC")

    offset := (req.Page - 1) * req.PageSize
    if err := query.Offset(int(offset)).Limit(int(req.PageSize)).Find(&parcels).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list parcels")
    }

    var total int64
    s.DB.Model(&Parcel{}).Where("sender_id = ?", req.UserId).Count(&total)

    var pbParcels []*pb.ParcelResponse
    for _, p := range parcels {
        pbParcels = append(pbParcels, &pb.ParcelResponse{
            Id:             p.ID,
            TrackingNumber: p.TrackingNumber,
            Status:         p.Status,
            Fee:            p.Fee,
            CreatedAt:      p.CreatedAt.String(),
        })
    }

    return &pb.ListParcelsResponse{
        Parcels: pbParcels,
        Total:   int32(total),
    }, nil
}

// ListCourierParcels lists parcels assigned to a courier
func (s *CourierServer) ListCourierParcels(ctx context.Context, req *pb.ListCourierParcelsRequest) (*pb.ListParcelsResponse, error) {
    var parcels []Parcel
    query := s.DB.Where("courier_id = ?", req.CourierId).Order("created_at DESC")

    offset := (req.Page - 1) * req.PageSize
    if err := query.Offset(int(offset)).Limit(int(req.PageSize)).Find(&parcels).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list parcels")
    }

    var total int64
    s.DB.Model(&Parcel{}).Where("courier_id = ?", req.CourierId).Count(&total)

    var pbParcels []*pb.ParcelResponse
    for _, p := range parcels {
        pbParcels = append(pbParcels, &pb.ParcelResponse{
            Id:             p.ID,
            TrackingNumber: p.TrackingNumber,
            Status:         p.Status,
            Fee:            p.Fee,
            CreatedAt:      p.CreatedAt.String(),
        })
    }

    return &pb.ListParcelsResponse{
        Parcels: pbParcels,
        Total:   int32(total),
    }, nil
}

// GetTrackingInfo returns tracking information
func (s *CourierServer) GetTrackingInfo(ctx context.Context, req *pb.GetTrackingInfoRequest) (*pb.TrackingResponse, error) {
    var parcel Parcel
    if err := s.DB.Where("tracking_number = ?", req.TrackingNumber).First(&parcel).Error; err != nil {
        return nil, status.Error(codes.NotFound, "parcel not found")
    }

    var events []*pb.TrackingEvent
    // In production, you would have a tracking events table
    events = append(events, &pb.TrackingEvent{
        Status:      parcel.Status,
        Location:    parcel.SenderAddress,
        Timestamp:   parcel.CreatedAt.Unix(),
        Description: "Parcel created",
    })

    return &pb.TrackingResponse{
        TrackingNumber: parcel.TrackingNumber,
        Status:         parcel.Status,
        EstimatedDelivery: parcel.CreatedAt.Add(24 * time.Hour).Unix(),
        Events:         events,
    }, nil
}

// calculateFee calculates delivery fee based on distance, weight, package type
func calculateFee(req *pb.CreateParcelRequest) float64 {
    // Simplified fee calculation
    // In production: use distance matrix API to calculate actual distance
    baseFee := 3.0
    weightFee := req.WeightKg * 0.5
    var typeFee float64
    switch req.PackageType {
    case "document":
        typeFee = 0
    case "box":
        typeFee = 2.0
    case "fragile":
        typeFee = 3.0
    case "large":
        typeFee = 5.0
    default:
        typeFee = 1.0
    }
    return baseFee + weightFee + typeFee
}

func generateID() string {
    return "cour_" + time.Now().Format("20060102150405") + "_" + randomString(6)
}

func generateTrackingNumber() string {
    return "TRK" + time.Now().Format("20060102") + randomString(8)
}

func randomString(n int) string {
    const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
    b := make([]byte, n)
    for i := range b {
        b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
    }
    return string(b)
}

func main() {
    godotenv.Load()

    dsn := os.Getenv("DB_DSN")
    if dsn == "" {
        dsn = "host=postgres user=postgres password=postgres dbname=courierdb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&Parcel{}, &DeliveryProof{})

    grpcServer := grpc.NewServer()
    pb.RegisterCourierServiceServer(grpcServer, &CourierServer{DB: db})

    lis, err := net.Listen("tcp", ":50058")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Courier Service running on port 50058")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("Courier Service stopped")
}
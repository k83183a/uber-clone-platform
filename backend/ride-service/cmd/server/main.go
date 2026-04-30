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

    pb "github.com/uber-clone/ride-service/proto"
)

type Ride struct {
    ID              string    `gorm:"primaryKey"`
    RiderID         string    `gorm:"index;not null"`
    DriverID        string    `gorm:"index"`
    Status          string    `gorm:"default:'pending'"`
    PickupLat       float64   `gorm:"not null"`
    PickupLng       float64   `gorm:"not null"`
    PickupAddress   string
    DropoffLat      float64   `gorm:"not null"`
    DropoffLng      float64   `gorm:"not null"`
    DropoffAddress  string
    RideType        string    `gorm:"default:'uberX'"`
    FareEstimate    float64
    FinalFare       float64
    DistanceKm      float64
    DurationMin     int
    PaymentMethod   string
    CreatedAt       time.Time
    AcceptedAt      *time.Time
    StartedAt       *time.Time
    CompletedAt     *time.Time
    CancelledAt     *time.Time
    CancelledBy     string
    CancelledReason string
}

type RideServer struct {
    pb.UnimplementedRideServiceServer
    DB *gorm.DB
}

func (s *RideServer) RequestRide(ctx context.Context, req *pb.RequestRideRequest) (*pb.RideResponse, error) {
    // Validate required fields
    if req.RiderId == "" {
        return nil, status.Error(codes.InvalidArgument, "rider_id is required")
    }
    if req.PickupLat == 0 || req.PickupLng == 0 {
        return nil, status.Error(codes.InvalidArgument, "pickup location is required")
    }
    if req.DropoffLat == 0 || req.DropoffLng == 0 {
        return nil, status.Error(codes.InvalidArgument, "dropoff location is required")
    }

    // Create ride record
    ride := &Ride{
        ID:             generateRideID(),
        RiderID:        req.RiderId,
        Status:         "pending",
        PickupLat:      req.PickupLat,
        PickupLng:      req.PickupLng,
        PickupAddress:  req.PickupAddress,
        DropoffLat:     req.DropoffLat,
        DropoffLng:     req.DropoffLng,
        DropoffAddress: req.DropoffAddress,
        RideType:       req.RideType,
        FareEstimate:   req.FareEstimate,
        PaymentMethod:  req.PaymentMethod,
        CreatedAt:      time.Now(),
    }

    if ride.RideType == "" {
        ride.RideType = "uberX"
    }

    if err := s.DB.Create(ride).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create ride")
    }

    // In production: publish to Kafka for dispatch service
    // s.kafka.PublishRideRequested(ride.ID, ride.RiderID, ride.PickupLat, ride.PickupLng)

    return &pb.RideResponse{
        Id:        ride.ID,
        Status:    ride.Status,
        Fare:      ride.FareEstimate,
        CreatedAt: ride.CreatedAt.String(),
    }, nil
}

func (s *RideServer) GetRide(ctx context.Context, req *pb.GetRideRequest) (*pb.RideDetailResponse, error) {
    var ride Ride
    if err := s.DB.Where("id = ?", req.RideId).First(&ride).Error; err != nil {
        return nil, status.Error(codes.NotFound, "ride not found")
    }

    return &pb.RideDetailResponse{
        Id:             ride.ID,
        RiderId:        ride.RiderID,
        DriverId:       ride.DriverID,
        Status:         ride.Status,
        PickupLat:      ride.PickupLat,
        PickupLng:      ride.PickupLng,
        PickupAddress:  ride.PickupAddress,
        DropoffLat:     ride.DropoffLat,
        DropoffLng:     ride.DropoffLng,
        DropoffAddress: ride.DropoffAddress,
        RideType:       ride.RideType,
        FareEstimate:   ride.FareEstimate,
        FinalFare:      ride.FinalFare,
        CreatedAt:      ride.CreatedAt.String(),
    }, nil
}

func (s *RideServer) AcceptRide(ctx context.Context, req *pb.AcceptRideRequest) (*pb.RideResponse, error) {
    var ride Ride
    if err := s.DB.Where("id = ? AND status = ?", req.RideId, "pending").First(&ride).Error; err != nil {
        return nil, status.Error(codes.NotFound, "ride not found or already accepted")
    }

    now := time.Now()
    ride.DriverID = req.DriverId
    ride.Status = "accepted"
    ride.AcceptedAt = &now

    if err := s.DB.Save(&ride).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to accept ride")
    }

    // In production: publish to Kafka
    // s.kafka.PublishRideAccepted(ride.ID, ride.DriverID)

    return &pb.RideResponse{
        Id:     ride.ID,
        Status: ride.Status,
        Fare:   ride.FareEstimate,
    }, nil
}

func (s *RideServer) StartRide(ctx context.Context, req *pb.StartRideRequest) (*pb.RideResponse, error) {
    var ride Ride
    if err := s.DB.Where("id = ? AND driver_id = ?", req.RideId, req.DriverId).First(&ride).Error; err != nil {
        return nil, status.Error(codes.NotFound, "ride not found")
    }

    if ride.Status != "accepted" {
        return nil, status.Error(codes.FailedPrecondition, "ride not accepted yet")
    }

    now := time.Now()
    ride.Status = "in_progress"
    ride.StartedAt = &now

    if err := s.DB.Save(&ride).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to start ride")
    }

    return &pb.RideResponse{
        Id:     ride.ID,
        Status: ride.Status,
    }, nil
}

func (s *RideServer) CompleteRide(ctx context.Context, req *pb.CompleteRideRequest) (*pb.RideResponse, error) {
    var ride Ride
    if err := s.DB.Where("id = ? AND driver_id = ?", req.RideId, req.DriverId).First(&ride).Error; err != nil {
        return nil, status.Error(codes.NotFound, "ride not found")
    }

    if ride.Status != "in_progress" {
        return nil, status.Error(codes.FailedPrecondition, "ride not started yet")
    }

    now := time.Now()
    ride.Status = "completed"
    ride.CompletedAt = &now
    ride.FinalFare = req.FinalFare
    ride.DistanceKm = req.DistanceKm
    ride.DurationMin = int(req.DurationMin)

    if err := s.DB.Save(&ride).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to complete ride")
    }

    // In production: publish to Kafka for payment service
    // s.kafka.PublishRideCompleted(ride.ID, ride.RiderID, ride.DriverID, ride.FinalFare)

    return &pb.RideResponse{
        Id:     ride.ID,
        Status: ride.Status,
        Fare:   ride.FinalFare,
    }, nil
}

func (s *RideServer) CancelRide(ctx context.Context, req *pb.CancelRideRequest) (*pb.Empty, error) {
    var ride Ride
    if err := s.DB.Where("id = ?", req.RideId).First(&ride).Error; err != nil {
        return nil, status.Error(codes.NotFound, "ride not found")
    }

    if ride.Status != "pending" && ride.Status != "accepted" {
        return nil, status.Error(codes.FailedPrecondition, "ride cannot be cancelled")
    }

    now := time.Now()
    ride.Status = "cancelled"
    ride.CancelledAt = &now
    ride.CancelledBy = req.CancelledBy
    ride.CancelledReason = req.Reason

    if err := s.DB.Save(&ride).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to cancel ride")
    }

    return &pb.Empty{}, nil
}

func (s *RideServer) ListRiderRides(ctx context.Context, req *pb.ListRidesRequest) (*pb.ListRidesResponse, error) {
    var rides []Ride
    query := s.DB.Where("rider_id = ?", req.UserId).Order("created_at DESC")

    if req.Limit > 0 {
        query = query.Limit(int(req.Limit))
    }
    if req.Offset > 0 {
        query = query.Offset(int(req.Offset))
    }

    if err := query.Find(&rides).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list rides")
    }

    var responses []*pb.RideResponse
    for _, r := range rides {
        responses = append(responses, &pb.RideResponse{
            Id:        r.ID,
            Status:    r.Status,
            Fare:      r.FinalFare,
            CreatedAt: r.CreatedAt.String(),
        })
    }

    return &pb.ListRidesResponse{Rides: responses}, nil
}

func (s *RideServer) ListDriverRides(ctx context.Context, req *pb.ListRidesRequest) (*pb.ListRidesResponse, error) {
    var rides []Ride
    query := s.DB.Where("driver_id = ?", req.UserId).Order("created_at DESC")

    if req.Limit > 0 {
        query = query.Limit(int(req.Limit))
    }
    if req.Offset > 0 {
        query = query.Offset(int(req.Offset))
    }

    if err := query.Find(&rides).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list rides")
    }

    var responses []*pb.RideResponse
    for _, r := range rides {
        responses = append(responses, &pb.RideResponse{
            Id:        r.ID,
            Status:    r.Status,
            Fare:      r.FinalFare,
            CreatedAt: r.CreatedAt.String(),
        })
    }

    return &pb.ListRidesResponse{Rides: responses}, nil
}

func generateRideID() string {
    return "ride_" + time.Now().Format("20060102150405") + "_" + randomString(6)
}

func randomString(n int) string {
    const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
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
        dsn = "host=postgres user=postgres password=postgres dbname=ridedb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&Ride{})

    grpcServer := grpc.NewServer()
    pb.RegisterRideServiceServer(grpcServer, &RideServer{DB: db})

    lis, err := net.Listen("tcp", ":50053")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Ride Service running on port 50053")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("Ride Service stopped")
}
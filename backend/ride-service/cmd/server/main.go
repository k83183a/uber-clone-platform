package main

import (
    "context"
    "log"
    "math"
    "net"
    "os"
    "os/signal"
    "sync"
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

// ============================================================
// MODELS
// ============================================================

type Ride struct {
    ID              string     `gorm:"primaryKey"`
    RiderID         string     `gorm:"index;not null"`
    DriverID        string     `gorm:"index"`
    Status          string     `gorm:"default:'pending'"`
    PickupLat       float64    `gorm:"not null"`
    PickupLng       float64    `gorm:"not null"`
    PickupAddress   string
    DropoffLat      float64    `gorm:"not null"`
    DropoffLng      float64    `gorm:"not null"`
    DropoffAddress  string
    RideType        string     `gorm:"default:'uberX'"`
    FareEstimate    float64
    FinalFare       float64
    DistanceKm      float64
    DurationMin     int
    SurgeMultiplier float64    `gorm:"default:1.0"`
    PaymentMethod   string
    OperatorID      string
    BusinessModel   string
    CreatedAt       time.Time
    AcceptedAt      *time.Time
    StartedAt       *time.Time
    CompletedAt     *time.Time
    CancelledAt     *time.Time
    CancelledBy     string
    CancelledReason string
}

// PricingConfig holds dynamic pricing rules per city/zone
type PricingConfig struct {
    City       string
    RideType   string
    BaseFare   float64
    PerKm      float64
    PerMinute  float64
}

// ============================================================
// GRPC SERVER
// ============================================================

type RideServer struct {
    pb.UnimplementedRideServiceServer
    DB              *gorm.DB
    pricingCache    map[string]PricingConfig
    pricingCacheMu  sync.RWMutex
    lastPricingLoad time.Time
}

// ============================================================
// PRICING CONFIGURATION
// ============================================================

func (s *RideServer) loadPricingConfigs() {
    // In production, load from database or config service
    configs := []PricingConfig{
        // London pricing
        {City: "london", RideType: "uberX", BaseFare: 3.50, PerKm: 1.25, PerMinute: 0.25},
        {City: "london", RideType: "uberXL", BaseFare: 5.00, PerKm: 1.80, PerMinute: 0.35},
        {City: "london", RideType: "green", BaseFare: 4.00, PerKm: 1.50, PerMinute: 0.30},
        {City: "london", RideType: "pet", BaseFare: 4.00, PerKm: 1.40, PerMinute: 0.30},
        {City: "london", RideType: "access", BaseFare: 4.00, PerKm: 1.40, PerMinute: 0.30},
        // Birmingham pricing (agent model)
        {City: "birmingham", RideType: "uberX", BaseFare: 2.80, PerKm: 1.25, PerMinute: 0.25},
        {City: "birmingham", RideType: "uberXL", BaseFare: 4.20, PerKm: 1.80, PerMinute: 0.35},
    }

    s.pricingCacheMu.Lock()
    s.pricingCache = make(map[string]PricingConfig)
    for _, cfg := range configs {
        key := cfg.City + ":" + cfg.RideType
        s.pricingCache[key] = cfg
    }
    s.pricingCacheMu.Unlock()
    s.lastPricingLoad = time.Now()
    log.Println("✅ Loaded pricing configurations")
}

func (s *RideServer) getPricingConfig(city, rideType string) PricingConfig {
    s.pricingCacheMu.RLock()
    defer s.pricingCacheMu.RUnlock()
    key := city + ":" + rideType
    if cfg, ok := s.pricingCache[key]; ok {
        return cfg
    }
    // Default fallback (London UberX)
    return PricingConfig{City: "london", RideType: "uberX", BaseFare: 3.50, PerKm: 1.25, PerMinute: 0.25}
}

// ============================================================
// SURGE PRICING (DEMAND-BASED)
// ============================================================

func (s *RideServer) getSurgeMultiplier(ctx context.Context, city string, pickupLat, pickupLng float64) float64 {
    // In production: query dispatch-service for nearby driver density
    // For MVP: surge based on time of day
    
    hour := time.Now().Hour()
    // Peak hours (7-9am and 5-7pm) = 1.5x surge
    if (hour >= 7 && hour <= 9) || (hour >= 17 && hour <= 19) {
        return 1.5
    }
    // Late night (11pm-5am) = 1.3x surge
    if hour >= 23 || hour <= 5 {
        return 1.3
    }
    return 1.0
}

// ============================================================
// CLEAN AIR ZONE SURCHARGE
// ============================================================

func (s *RideServer) getCazSurcharge(lat, lng float64) float64 {
    // In production: query geofencing-service to check if within CAZ
    // For MVP: return 2.50 if coordinates in London area
    if lat > 51.45 && lat < 51.55 && lng > -0.15 && lng < -0.05 {
        return 2.50 // London CAZ
    }
    return 0
}

// ============================================================
// RIDE LIFECYCLE METHODS
// ============================================================

// RequestRide - Rider creates a new ride request
func (s *RideServer) RequestRide(ctx context.Context, req *pb.RequestRideRequest) (*pb.RideResponse, error) {
    // Calculate fare
    city := s.getCityFromCoords(req.PickupLat, req.PickupLng)
    pricing := s.getPricingConfig(city, req.RideType)
    
    // Estimate distance (simplified - use routing service in production)
    estDistance := s.calculateDistance(req.PickupLat, req.PickupLng, req.DropoffLat, req.DropoffLng)
    estDuration := estDistance / 0.5 // Average speed 30 km/h = 0.5 km/min
    
    // Base fare calculation
    fare := pricing.BaseFare + (pricing.PerKm * estDistance) + (pricing.PerMinute * estDuration)
    
    // Surge multiplier
    surgeMultiplier := s.getSurgeMultiplier(ctx, city, req.PickupLat, req.PickupLng)
    fare = fare * surgeMultiplier
    
    // CAZ surcharge
    cazSurcharge := s.getCazSurcharge(req.PickupLat, req.PickupLng)
    if cazSurcharge > 0 {
        fare += cazSurcharge
    }
    
    // Apply subscription discount (10% if subscriber)
    if req.IsSubscriber {
        fare = fare * 0.9
    }
    
    // Apply promotion discount
    if req.PromoDiscount > 0 {
        fare = fare - req.PromoDiscount
        if fare < 0 {
            fare = 0
        }
    }
    
    ride := &Ride{
        ID:              generateRideID(),
        RiderID:         req.RiderId,
        Status:          "pending",
        PickupLat:       req.PickupLat,
        PickupLng:       req.PickupLng,
        PickupAddress:   req.PickupAddress,
        DropoffLat:      req.DropoffLat,
        DropoffLng:      req.DropoffLng,
        DropoffAddress:  req.DropoffAddress,
        RideType:        req.RideType,
        FareEstimate:    fare,
        SurgeMultiplier: surgeMultiplier,
        PaymentMethod:   req.PaymentMethod,
        OperatorID:      req.OperatorId,
        BusinessModel:   req.BusinessModel,
        CreatedAt:       time.Now(),
    }
    
    if err := s.DB.Create(ride).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create ride")
    }
    
    // In production: publish to Kafka for dispatch service
    log.Printf("🚗 Ride requested: %s - Estimated fare: £%.2f", ride.ID, fare)
    
    return &pb.RideResponse{
        Id:        ride.ID,
        Status:    ride.Status,
        Fare:      ride.FareEstimate,
        CreatedAt: ride.CreatedAt.Unix(),
    }, nil
}

// GetRide - Retrieve ride details
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
        SurgeMultiplier: ride.SurgeMultiplier,
        CreatedAt:      ride.CreatedAt.Unix(),
    }, nil
}

// AcceptRide - Driver accepts a ride
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
    
    // In production: send notification to rider via Kafka
    log.Printf("✅ Ride %s accepted by driver %s", ride.ID, req.DriverId)
    
    return &pb.RideResponse{
        Id:     ride.ID,
        Status: ride.Status,
        Fare:   ride.FareEstimate,
    }, nil
}

// StartRide - Driver starts the ride (arrived at pickup)
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
    
    log.Printf("🚦 Ride %s started", ride.ID)
    
    return &pb.RideResponse{
        Id:     ride.ID,
        Status: ride.Status,
    }, nil
}

// CompleteRide - Driver completes the ride (arrived at destination)
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
    log.Printf("🏁 Ride %s completed. Final fare: £%.2f", ride.ID, req.FinalFare)
    
    return &pb.RideResponse{
        Id:     ride.ID,
        Status: ride.Status,
        Fare:   ride.FinalFare,
    }, nil
}

// CancelRide - Cancel a ride (rider or driver)
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
    
    log.Printf("❌ Ride %s cancelled by %s: %s", ride.ID, req.CancelledBy, req.Reason)
    
    return &pb.Empty{}, nil
}

// ListRiderRides - Get ride history for a rider
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
            CreatedAt: r.CreatedAt.Unix(),
        })
    }
    
    return &pb.ListRidesResponse{Rides: responses}, nil
}

// ListDriverRides - Get ride history for a driver
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
            CreatedAt: r.CreatedAt.Unix(),
        })
    }
    
    return &pb.ListRidesResponse{Rides: responses}, nil
}

// GetActiveRideForRider - Get current active ride for a rider
func (s *RideServer) GetActiveRideForRider(ctx context.Context, req *pb.GetActiveRideRequest) (*pb.RideDetailResponse, error) {
    var ride Ride
    if err := s.DB.Where("rider_id = ? AND status IN ?", req.UserId, []string{"pending", "accepted", "in_progress"}).First(&ride).Error; err != nil {
        return nil, status.Error(codes.NotFound, "no active ride found")
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
        CreatedAt:      ride.CreatedAt.Unix(),
    }, nil
}

// GetActiveRideForDriver - Get current active ride for a driver
func (s *RideServer) GetActiveRideForDriver(ctx context.Context, req *pb.GetActiveRideRequest) (*pb.RideDetailResponse, error) {
    var ride Ride
    if err := s.DB.Where("driver_id = ? AND status IN ?", req.UserId, []string{"accepted", "in_progress"}).First(&ride).Error; err != nil {
        return nil, status.Error(codes.NotFound, "no active ride found")
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
        CreatedAt:      ride.CreatedAt.Unix(),
    }, nil
}

// ============================================================
// HELPER FUNCTIONS
// ============================================================

func (s *RideServer) calculateDistance(lat1, lng1, lat2, lng2 float64) float64 {
    // Haversine formula - approximate distance in km
    const R = 6371 // Earth's radius in km
    lat1Rad := lat1 * math.Pi / 180
    lat2Rad := lat2 * math.Pi / 180
    dLat := (lat2 - lat1) * math.Pi / 180
    dLng := (lng2 - lng1) * math.Pi / 180
    
    a := math.Sin(dLat/2)*math.Sin(dLat/2) +
        math.Cos(lat1Rad)*math.Cos(lat2Rad)*
            math.Sin(dLng/2)*math.Sin(dLng/2)
    c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
    
    return R * c
}

func (s *RideServer) getCityFromCoords(lat, lng float64) string {
    // Simple city detection based on coordinates
    // In production: call geofencing-service
    if lat > 51.45 && lat < 51.55 && lng > -0.15 && lng < -0.05 {
        return "london"
    }
    if lat > 52.45 && lat < 52.55 && lng > -1.95 && lng < -1.85 {
        return "birmingham"
    }
    return "london"
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

// ============================================================
// MAIN
// ============================================================

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
    
    server := &RideServer{DB: db}
    server.loadPricingConfigs()
    
    grpcServer := grpc.NewServer()
    pb.RegisterRideServiceServer(grpcServer, server)
    
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
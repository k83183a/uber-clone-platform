package main

import (
    "context"
    "encoding/json"
    "log"
    "math"
    "net"
    "os"
    "os/signal"
    "strconv"
    "sync"
    "syscall"
    "time"

    "github.com/joho/godotenv"
    "github.com/redis/go-redis/v9"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"

    pb "github.com/uber-clone/dispatch-service/proto"
)

// ============================================================
// MODELS
// ============================================================

type DriverLocation struct {
    DriverID  string    `json:"driver_id"`
    Lat       float64   `json:"lat"`
    Lng       float64   `json:"lng"`
    Status    string    `json:"status"` // online, offline, on_trip
    UpdatedAt time.Time `json:"updated_at"`
}

type PendingRide struct {
    RideID    string  `json:"ride_id"`
    PickupLat float64 `json:"pickup_lat"`
    PickupLng float64 `json:"pickup_lng"`
    Zone      string  `json:"zone"`
}

// ============================================================
// GRPC SERVER
// ============================================================

type DispatchServer struct {
    pb.UnimplementedDispatchServiceServer
    redisClient *redis.Client
    mu          sync.RWMutex
    pendingRides map[string]*PendingRide
}

// UpdateDriverLocation - Update driver's location in Redis GEO
func (s *DispatchServer) UpdateDriverLocation(ctx context.Context, req *pb.UpdateDriverLocationRequest) (*pb.UpdateResponse, error) {
    key := "drivers:locations"

    // Update Redis GEO set
    err := s.redisClient.GeoAdd(ctx, key, &redis.GeoLocation{
        Name:      req.DriverId,
        Longitude: req.Longitude,
        Latitude:  req.Latitude,
    }).Err()
    if err != nil {
        return nil, status.Error(codes.Internal, "failed to update location")
    }

    // Store driver status in hash
    driverData := DriverLocation{
        DriverID:  req.DriverId,
        Lat:       req.Latitude,
        Lng:       req.Longitude,
        Status:    req.Status,
        UpdatedAt: time.Now(),
    }
    dataJSON, _ := json.Marshal(driverData)
    err = s.redisClient.HSet(ctx, "drivers:status", req.DriverId, dataJSON).Err()
    if err != nil {
        log.Printf("Failed to store driver status: %v", err)
    }

    // Set expiry on GEO member (1 hour)
    s.redisClient.Expire(ctx, key, time.Hour)

    return &pb.UpdateResponse{Success: true}, nil
}

// FindNearestDrivers - Find nearest drivers using Redis GEO
func (s *DispatchServer) FindNearestDrivers(ctx context.Context, req *pb.FindNearestDriversRequest) (*pb.FindNearestDriversResponse, error) {
    key := "drivers:locations"

    query := &redis.GeoRadiusQuery{
        Radius:    req.RadiusKm,
        Unit:      "km",
        WithCoord: true,
        WithDist:  true,
        Count:     int(req.Limit),
        Sort:      "ASC",
    }

    results, err := s.redisClient.GeoRadius(ctx, key, req.Longitude, req.Latitude, query).Result()
    if err != nil {
        return nil, status.Error(codes.Internal, "failed to find drivers")
    }

    var drivers []*pb.NearbyDriver
    for _, result := range results {
        // Check driver status (online and not on trip)
        statusJSON, err := s.redisClient.HGet(ctx, "drivers:status", result.Name).Result()
        if err == nil {
            var driverData DriverLocation
            json.Unmarshal([]byte(statusJSON), &driverData)
            if driverData.Status != "online" {
                continue
            }
        }

        drivers = append(drivers, &pb.NearbyDriver{
            DriverId:   result.Name,
            DistanceKm: result.Dist,
            Latitude:   result.Latitude,
            Longitude:  result.Longitude,
        })
    }

    return &pb.FindNearestDriversResponse{Drivers: drivers}, nil
}

// FindNearestDriversForZone - Find drivers within a specific zone
func (s *DispatchServer) FindNearestDriversForZone(ctx context.Context, req *pb.FindNearestDriversForZoneRequest) (*pb.FindNearestDriversResponse, error) {
    // In production: get zone polygon from geofencing service
    // For MVP, use center point and radius
    centerLat, centerLng := s.getZoneCenter(req.ZoneName)
    radius := 10.0 // km

    return s.FindNearestDrivers(ctx, &pb.FindNearestDriversRequest{
        Latitude:  centerLat,
        Longitude: centerLng,
        RadiusKm:  radius,
        Limit:     req.Limit,
    })
}

// GetDriverStatus - Get a driver's current status
func (s *DispatchServer) GetDriverStatus(ctx context.Context, req *pb.GetDriverStatusRequest) (*pb.GetDriverStatusResponse, error) {
    statusJSON, err := s.redisClient.HGet(ctx, "drivers:status", req.DriverId).Result()
    if err != nil {
        return &pb.GetDriverStatusResponse{Status: "offline"}, nil
    }

    var driverData DriverLocation
    json.Unmarshal([]byte(statusJSON), &driverData)

    return &pb.GetDriverStatusResponse{
        Status:     driverData.Status,
        Latitude:   driverData.Lat,
        Longitude:  driverData.Lng,
        UpdatedAt:  driverData.UpdatedAt.Unix(),
    }, nil
}

// SetDriverStatus - Set driver online/offline
func (s *DispatchServer) SetDriverStatus(ctx context.Context, req *pb.SetDriverStatusRequest) (*pb.UpdateResponse, error) {
    // Get existing location if available
    var lat, lng float64
    statusJSON, err := s.redisClient.HGet(ctx, "drivers:status", req.DriverId).Result()
    if err == nil {
        var existing DriverLocation
        json.Unmarshal([]byte(statusJSON), &existing)
        lat = existing.Lat
        lng = existing.Lng
    }

    driverData := DriverLocation{
        DriverID:  req.DriverId,
        Lat:       lat,
        Lng:       lng,
        Status:    req.Status,
        UpdatedAt: time.Now(),
    }
    dataJSON, _ := json.Marshal(driverData)
    err = s.redisClient.HSet(ctx, "drivers:status", req.DriverId, dataJSON).Err()
    if err != nil {
        return nil, status.Error(codes.Internal, "failed to set driver status")
    }

    // Update GEO set if online and we have coordinates
    if req.Status == "online" && lat != 0 && lng != 0 {
        key := "drivers:locations"
        s.redisClient.GeoAdd(ctx, key, &redis.GeoLocation{
            Name:      req.DriverId,
            Longitude: lng,
            Latitude:  lat,
        })
    } else if req.Status != "online" {
        // Remove from GEO set if offline
        s.redisClient.ZRem(ctx, "drivers:locations", req.DriverId)
    }

    return &pb.UpdateResponse{Success: true}, nil
}

// GetActiveDriverCount - Get count of online drivers
func (s *DispatchServer) GetActiveDriverCount(ctx context.Context, req *pb.GetActiveDriverCountRequest) (*pb.GetActiveDriverCountResponse, error) {
    count, err := s.redisClient.ZCard(ctx, "drivers:locations").Result()
    if err != nil {
        return nil, status.Error(codes.Internal, "failed to get driver count")
    }

    return &pb.GetActiveDriverCountResponse{Count: int32(count)}, nil
}

// GetSurgeMultiplier - Calculate surge multiplier based on demand/supply
func (s *DispatchServer) GetSurgeMultiplier(ctx context.Context, req *pb.GetSurgeMultiplierRequest) (*pb.GetSurgeMultiplierResponse, error) {
    // Find nearby drivers
    drivers, err := s.FindNearestDrivers(ctx, &pb.FindNearestDriversRequest{
        Latitude:  req.Latitude,
        Longitude: req.Longitude,
        RadiusKm:  3.0,
        Limit:     50,
    })
    if err != nil {
        return &pb.GetSurgeMultiplierResponse{Multiplier: 1.0}, nil
    }

    // Count pending rides in this zone
    pendingCount := s.getPendingRideCount(req.Zone)

    driverCount := len(drivers.Drivers)
    multiplier := 1.0

    if driverCount < 5 {
        multiplier = 2.5
    } else if driverCount < 10 {
        multiplier = 2.0
    } else if driverCount < 20 {
        multiplier = 1.5
    } else if driverCount < 30 {
        multiplier = 1.2
    }

    // Adjust based on pending rides
    if pendingCount > 10 {
        multiplier = math.Min(multiplier*1.5, 3.0)
    }

    // Time-based surge (peak hours)
    hour := time.Now().Hour()
    if (hour >= 7 && hour <= 9) || (hour >= 17 && hour <= 19) {
        multiplier = math.Max(multiplier, 1.5)
    }

    return &pb.GetSurgeMultiplierResponse{Multiplier: multiplier}, nil
}

// RequestDispatch - Dispatch a ride to nearest driver
func (s *DispatchServer) RequestDispatch(ctx context.Context, req *pb.RequestDispatchRequest) (*pb.DispatchResponse, error) {
    // Find nearest driver
    drivers, err := s.FindNearestDrivers(ctx, &pb.FindNearestDriversRequest{
        Latitude:  req.PickupLat,
        Longitude: req.PickupLng,
        RadiusKm:  10.0,
        Limit:     1,
    })
    if err != nil || len(drivers.Drivers) == 0 {
        return &pb.DispatchResponse{
            Success: false,
            Message: "No drivers available nearby",
        }, nil
    }

    driverID := drivers.Drivers[0].DriverId

    // Mark driver as on_trip
    s.SetDriverStatus(ctx, &pb.SetDriverStatusRequest{
        DriverId: driverID,
        Status:   "on_trip",
    })

    // Store pending ride for this driver
    s.mu.Lock()
    s.pendingRides[driverID] = &PendingRide{
        RideID:    req.RideId,
        PickupLat: req.PickupLat,
        PickupLng: req.PickupLng,
        Zone:      req.Zone,
    }
    s.mu.Unlock()

    return &pb.DispatchResponse{
        Success:   true,
        DriverId:  driverID,
        Message:   "Driver dispatched successfully",
    }, nil
}

// CompleteDispatch - Mark a ride as completed (free up driver)
func (s *DispatchServer) CompleteDispatch(ctx context.Context, req *pb.CompleteDispatchRequest) (*pb.UpdateResponse, error) {
    s.mu.Lock()
    delete(s.pendingRides, req.DriverId)
    s.mu.Unlock()

    // Set driver back to online
    s.SetDriverStatus(ctx, &pb.SetDriverStatusRequest{
        DriverId: req.DriverId,
        Status:   "online",
    })

    return &pb.UpdateResponse{Success: true}, nil
}

// GetDriverETA - Calculate ETA for driver to pickup
func (s *DispatchServer) GetDriverETA(ctx context.Context, req *pb.GetDriverETARequest) (*pb.ETAResponse, error) {
    // Get driver location
    statusJSON, err := s.redisClient.HGet(ctx, "drivers:status", req.DriverId).Result()
    if err != nil {
        return nil, status.Error(codes.NotFound, "driver not found")
    }

    var driverData DriverLocation
    json.Unmarshal([]byte(statusJSON), &driverData)

    // Calculate distance
    distance := haversine(driverData.Lat, driverData.Lng, req.PickupLat, req.PickupLng)
    // Assume average speed 30 km/h = 0.5 km/min
    etaMinutes := int(distance / 0.5)
    if etaMinutes < 1 {
        etaMinutes = 1
    }

    return &pb.ETAResponse{
        EtaMinutes: int32(etaMinutes),
        DriverLat:  driverData.Lat,
        DriverLng:  driverData.Lng,
    }, nil
}

// ============================================================
// HELPER FUNCTIONS
// ============================================================

func (s *DispatchServer) getZoneCenter(zoneName string) (float64, float64) {
    // In production: query geofencing service
    switch zoneName {
    case "london":
        return 51.5074, -0.1278
    case "birmingham":
        return 52.4862, -1.8904
    default:
        return 51.5074, -0.1278
    }
}

func (s *DispatchServer) getPendingRideCount(zone string) int {
    s.mu.RLock()
    defer s.mu.RUnlock()
    count := 0
    for _, ride := range s.pendingRides {
        if ride.Zone == zone {
            count++
        }
    }
    return count
}

func haversine(lat1, lon1, lat2, lon2 float64) float64 {
    const R = 6371 // Earth radius in km
    dLat := (lat2 - lat1) * math.Pi / 180
    dLon := (lon2 - lon1) * math.Pi / 180
    a := math.Sin(dLat/2)*math.Sin(dLat/2) +
        math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
            math.Sin(dLon/2)*math.Sin(dLon/2)
    c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
    return R * c
}

// Start background job to clean stale drivers
func (s *DispatchServer) startCleanupJob() {
    ticker := time.NewTicker(5 * time.Minute)
    go func() {
        for range ticker.C {
            ctx := context.Background()
            key := "drivers:locations"
            drivers, err := s.redisClient.ZRange(ctx, key, 0, -1).Result()
            if err != nil {
                continue
            }
            for _, driverID := range drivers {
                statusJSON, err := s.redisClient.HGet(ctx, "drivers:status", driverID).Result()
                if err != nil {
                    s.redisClient.ZRem(ctx, key, driverID)
                    continue
                }
                var driverData DriverLocation
                json.Unmarshal([]byte(statusJSON), &driverData)
                if time.Since(driverData.UpdatedAt) > 30*time.Minute {
                    s.redisClient.ZRem(ctx, key, driverID)
                    log.Printf("Removed stale driver %s", driverID)
                }
            }
        }
    }()
}

func main() {
    godotenv.Load()

    redisAddr := os.Getenv("REDIS_ADDR")
    if redisAddr == "" {
        redisAddr = "localhost:6379"
    }

    redisPassword := os.Getenv("REDIS_PASSWORD")

    rdb := redis.NewClient(&redis.Options{
        Addr:     redisAddr,
        Password: redisPassword,
        DB:       0,
        PoolSize: 100,
    })

    ctx := context.Background()
    if err := rdb.Ping(ctx).Err(); err != nil {
        log.Fatal("Failed to connect to Redis:", err)
    }
    log.Println("✅ Connected to Redis")

    server := &DispatchServer{
        redisClient:  rdb,
        pendingRides: make(map[string]*PendingRide),
    }
    server.startCleanupJob()

    grpcServer := grpc.NewServer()
    pb.RegisterDispatchServiceServer(grpcServer, server)

    lis, err := net.Listen("tcp", ":50060")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Dispatch Service running on port 50060")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("Dispatch Service stopped")
}
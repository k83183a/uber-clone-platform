package main

import (
    "context"
    "encoding/json"
    "log"
    "net"
    "os"
    "os/signal"
    "strconv"
    "syscall"
    "time"

    "github.com/joho/godotenv"
    "github.com/redis/go-redis/v9"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"

    pb "github.com/uber-clone/dispatch-service/proto"
)

// DriverLocation stores driver information
type DriverLocation struct {
    DriverID  string    `json:"driver_id"`
    Latitude  float64   `json:"lat"`
    Longitude float64   `json:"lng"`
    Status    string    `json:"status"` // online, offline, on_trip
    UpdatedAt time.Time `json:"updated_at"`
}

// DispatchServer handles gRPC requests
type DispatchServer struct {
    pb.UnimplementedDispatchServiceServer
    redisClient *redis.Client
}

// UpdateDriverLocation updates a driver's location in Redis (GEO)
func (s *DispatchServer) UpdateDriverLocation(ctx context.Context, req *pb.UpdateDriverLocationRequest) (*pb.UpdateResponse, error) {
    key := "drivers:locations"
    member := req.DriverId

    // Add to Redis GEO set
    err := s.redisClient.GeoAdd(ctx, key, &redis.GeoLocation{
        Name:      member,
        Longitude: req.Longitude,
        Latitude:  req.Latitude,
    }).Err()
    if err != nil {
        return nil, status.Error(codes.Internal, "failed to update location")
    }

    // Store driver status
    statusKey := "driver:" + req.DriverId + ":status"
    statusData := DriverLocation{
        DriverID:  req.DriverId,
        Latitude:  req.Latitude,
        Longitude: req.Longitude,
        Status:    req.Status,
        UpdatedAt: time.Now(),
    }
    statusJSON, _ := json.Marshal(statusData)
    err = s.redisClient.Set(ctx, statusKey, statusJSON, 24*time.Hour).Err()
    if err != nil {
        log.Printf("Failed to store driver status: %v", err)
    }

    // Set expiry on GEO member (24 hours)
    s.redisClient.Expire(ctx, key, 24*time.Hour)

    return &pb.UpdateResponse{Success: true}, nil
}

// FindNearestDrivers finds nearest drivers within radius
func (s *DispatchServer) FindNearestDrivers(ctx context.Context, req *pb.FindNearestDriversRequest) (*pb.FindNearestDriversResponse, error) {
    key := "drivers:locations"

    // Query Redis GEO for drivers within radius
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
        // Skip drivers who are offline or on trip
        statusKey := "driver:" + result.Name + ":status"
        statusJSON, err := s.redisClient.Get(ctx, statusKey).Result()
        if err == nil {
            var driverStatus DriverLocation
            json.Unmarshal([]byte(statusJSON), &driverStatus)
            if driverStatus.Status != "online" {
                continue
            }
        }

        drivers = append(drivers, &pb.NearbyDriver{
            DriverId:     result.Name,
            DistanceKm:   result.Dist,
            Latitude:     result.Latitude,
            Longitude:    result.Longitude,
        })
    }

    return &pb.FindNearestDriversResponse{Drivers: drivers}, nil
}

// GetDriverStatus gets a driver's current status
func (s *DispatchServer) GetDriverStatus(ctx context.Context, req *pb.GetDriverStatusRequest) (*pb.GetDriverStatusResponse, error) {
    statusKey := "driver:" + req.DriverId + ":status"
    statusJSON, err := s.redisClient.Get(ctx, statusKey).Result()
    if err != nil {
        return &pb.GetDriverStatusResponse{Status: "offline"}, nil
    }

    var driverStatus DriverLocation
    json.Unmarshal([]byte(statusJSON), &driverStatus)

    return &pb.GetDriverStatusResponse{
        Status:    driverStatus.Status,
        Latitude:  driverStatus.Latitude,
        Longitude: driverStatus.Longitude,
        UpdatedAt: driverStatus.UpdatedAt.Unix(),
    }, nil
}

// SetDriverStatus sets a driver's online/offline status
func (s *DispatchServer) SetDriverStatus(ctx context.Context, req *pb.SetDriverStatusRequest) (*pb.UpdateResponse, error) {
    statusKey := "driver:" + req.DriverId + ":status"

    // Get existing location if available
    var latitude, longitude float64
    existingJSON, err := s.redisClient.Get(ctx, statusKey).Result()
    if err == nil {
        var existing DriverLocation
        json.Unmarshal([]byte(existingJSON), &existing)
        latitude = existing.Latitude
        longitude = existing.Longitude
    }

    driverData := DriverLocation{
        DriverID:  req.DriverId,
        Latitude:  latitude,
        Longitude: longitude,
        Status:    req.Status,
        UpdatedAt: time.Now(),
    }
    dataJSON, _ := json.Marshal(driverData)

    err = s.redisClient.Set(ctx, statusKey, dataJSON, 24*time.Hour).Err()
    if err != nil {
        return nil, status.Error(codes.Internal, "failed to set driver status")
    }

    // If status is offline, remove from GEO index
    if req.Status == "offline" {
        key := "drivers:locations"
        s.redisClient.ZRem(ctx, key, req.DriverId)
    } else if req.Status == "online" && latitude != 0 && longitude != 0 {
        // Re-add to GEO index if going online and we have location
        key := "drivers:locations"
        s.redisClient.GeoAdd(ctx, key, &redis.GeoLocation{
            Name:      req.DriverId,
            Longitude: longitude,
            Latitude:  latitude,
        })
    }

    return &pb.UpdateResponse{Success: true}, nil
}

// GetActiveDriverCount returns count of online drivers
func (s *DispatchServer) GetActiveDriverCount(ctx context.Context, req *pb.GetActiveDriverCountRequest) (*pb.GetActiveDriverCountResponse, error) {
    key := "drivers:locations"
    count, err := s.redisClient.ZCard(ctx, key).Result()
    if err != nil {
        return nil, status.Error(codes.Internal, "failed to get driver count")
    }

    return &pb.GetActiveDriverCountResponse{Count: int32(count)}, nil
}

// GetSurgeMultiplier calculates surge pricing based on demand
func (s *DispatchServer) GetSurgeMultiplier(ctx context.Context, req *pb.GetSurgeMultiplierRequest) (*pb.GetSurgeMultiplierResponse, error) {
    // Find nearby drivers
    drivers, err := s.FindNearestDrivers(ctx, &pb.FindNearestDriversRequest{
        Latitude:  req.Latitude,
        Longitude: req.Longitude,
        RadiusKm:  5.0,
        Limit:     100,
    })
    if err != nil {
        return &pb.GetSurgeMultiplierResponse{Multiplier: 1.0}, nil
    }

    // Get recent ride requests in this area (from Redis or Kafka)
    // For MVP, use a simple algorithm based on driver density
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

    return &pb.GetSurgeMultiplierResponse{Multiplier: multiplier}, nil
}

// Cleanup offline drivers periodically
func (s *DispatchServer) cleanupOfflineDrivers() {
    ticker := time.NewTicker(5 * time.Minute)
    for range ticker.C {
        ctx := context.Background()
        key := "drivers:locations"

        // Get all drivers in GEO set
        drivers, err := s.redisClient.ZRange(ctx, key, 0, -1).Result()
        if err != nil {
            continue
        }

        for _, driverID := range drivers {
            statusKey := "driver:" + driverID + ":status"
            _, err := s.redisClient.Get(ctx, statusKey).Result()
            if err != nil {
                // No status found, driver is stale, remove from GEO
                s.redisClient.ZRem(ctx, key, driverID)
            }
        }
    }
}

func main() {
    godotenv.Load()

    // Connect to Redis
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

    // Start gRPC server
    grpcServer := grpc.NewServer()
    dispatchServer := &DispatchServer{
        redisClient: rdb,
    }
    pb.RegisterDispatchServiceServer(grpcServer, dispatchServer)

    // Start cleanup goroutine
    go dispatchServer.cleanupOfflineDrivers()

    port := os.Getenv("PORT")
    if port == "" {
        port = "50060"
    }

    lis, err := net.Listen("tcp", ":"+port)
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Printf("✅ Dispatch Service running on port %s", port)
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
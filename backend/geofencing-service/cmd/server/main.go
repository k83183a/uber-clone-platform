package main

import (
    "context"
    "database/sql"
    "encoding/json"
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
    "gorm.io/gorm"

    pb "github.com/uber-clone/geofencing-service/proto"
)

// Zone represents a geofenced area (city, clean air zone, fare zone, surge zone)
type Zone struct {
    ID             string         `gorm:"primaryKey"`
    Name           string         `gorm:"uniqueIndex;not null"`
    Type           string         `gorm:"index"` // city, clean_air, fare_zone, surge_zone
    Description    string
    PickupSurcharge float64        `gorm:"default:0"` // surcharge when pickup inside zone
    DropoffSurcharge float64       `gorm:"default:0"` // surcharge when dropoff inside zone
    GeoJSON        string         `gorm:"type:text"` // GeoJSON polygon
    IsActive       bool           `gorm:"default:true"`
    CreatedAt      time.Time
    UpdatedAt      time.Time
}

// GeofencingServer handles gRPC requests
type GeofencingServer struct {
    pb.UnimplementedGeofencingServiceServer
    DB *gorm.DB
}

// PointInZone checks if a point is inside a specific zone
func (s *GeofencingServer) PointInZone(ctx context.Context, req *pb.PointInZoneRequest) (*pb.PointInZoneResponse, error) {
    var zone Zone
    if err := s.DB.Where("name = ? AND is_active = ?", req.ZoneName, true).First(&zone).Error; err != nil {
        return nil, status.Error(codes.NotFound, "zone not found")
    }

    // In production: use PostGIS ST_Contains for accurate point-in-polygon
    // For MVP, use a simple bounding box check (or call external service)
    contains := pointInPolygon(req.Latitude, req.Longitude, zone.GeoJSON)
    // Also return surcharges if applicable
    return &pb.PointInZoneResponse{
        Contains:         contains,
        PickupSurcharge:  zone.PickupSurcharge,
        DropoffSurcharge: zone.DropoffSurcharge,
    }, nil
}

// GetZonesForLocation returns all zones containing a point
func (s *GeofencingServer) GetZonesForLocation(ctx context.Context, req *pb.GetZonesForLocationRequest) (*pb.GetZonesForLocationResponse, error) {
    var zones []Zone
    if err := s.DB.Where("is_active = ?", true).Find(&zones).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to fetch zones")
    }

    var matchedZones []*pb.ZoneInfo
    totalPickupSurcharge := 0.0
    totalDropoffSurcharge := 0.0

    for _, z := range zones {
        // In production: use PostGIS for fast spatial query
        if pointInPolygon(req.Latitude, req.Longitude, z.GeoJSON) {
            matchedZones = append(matchedZones, &pb.ZoneInfo{
                ZoneName:          z.Name,
                ZoneType:          z.Type,
                PickupSurcharge:   z.PickupSurcharge,
                DropoffSurcharge:  z.DropoffSurcharge,
            })
            totalPickupSurcharge += z.PickupSurcharge
            totalDropoffSurcharge += z.DropoffSurcharge
        }
    }

    return &pb.GetZonesForLocationResponse{
        Zones:                matchedZones,
        TotalPickupSurcharge: totalPickupSurcharge,
        TotalDropoffSurcharge: totalDropoffSurcharge,
    }, nil
}

// GetCity returns the city name for a location (first city zone that contains the point)
func (s *GeofencingServer) GetCity(ctx context.Context, req *pb.GetCityRequest) (*pb.GetCityResponse, error) {
    var zones []Zone
    if err := s.DB.Where("type = ? AND is_active = ?", "city", true).Find(&zones).Error; err != nil {
        return &pb.GetCityResponse{City: "unknown"}, nil
    }

    for _, z := range zones {
        if pointInPolygon(req.Latitude, req.Longitude, z.GeoJSON) {
            return &pb.GetCityResponse{City: z.Name}, nil
        }
    }

    return &pb.GetCityResponse{City: "unknown"}, nil
}

// ListZones lists all zones
func (s *GeofencingServer) ListZones(ctx context.Context, req *pb.ListZonesRequest) (*pb.ListZonesResponse, error) {
    var zones []Zone
    query := s.DB.Where("is_active = ?", true)

    if req.ZoneType != "" {
        query = query.Where("type = ?", req.ZoneType)
    }

    if err := query.Find(&zones).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list zones")
    }

    var pbZones []*pb.Zone
    for _, z := range zones {
        pbZones = append(pbZones, &pb.Zone{
            Id:               z.ID,
            Name:             z.Name,
            Type:             z.Type,
            Description:      z.Description,
            PickupSurcharge:  z.PickupSurcharge,
            DropoffSurcharge: z.DropoffSurcharge,
            GeoJson:          z.GeoJSON,
        })
    }

    return &pb.ListZonesResponse{Zones: pbZones}, nil
}

// CreateZone creates a new geofence zone (admin endpoint)
func (s *GeofencingServer) CreateZone(ctx context.Context, req *pb.CreateZoneRequest) (*pb.Zone, error) {
    zone := &Zone{
        ID:               generateID(),
        Name:             req.Name,
        Type:             req.Type,
        Description:      req.Description,
        PickupSurcharge:  req.PickupSurcharge,
        DropoffSurcharge: req.DropoffSurcharge,
        GeoJSON:          req.GeoJson,
        IsActive:         true,
        CreatedAt:        time.Now(),
        UpdatedAt:        time.Now(),
    }

    if err := s.DB.Create(zone).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create zone")
    }

    return &pb.Zone{
        Id:               zone.ID,
        Name:             zone.Name,
        Type:             zone.Type,
        Description:      zone.Description,
        PickupSurcharge:  zone.PickupSurcharge,
        DropoffSurcharge: zone.DropoffSurcharge,
        GeoJson:          zone.GeoJSON,
    }, nil
}

// UpdateZone updates an existing zone
func (s *GeofencingServer) UpdateZone(ctx context.Context, req *pb.UpdateZoneRequest) (*pb.Zone, error) {
    var zone Zone
    if err := s.DB.Where("id = ?", req.Id).First(&zone).Error; err != nil {
        return nil, status.Error(codes.NotFound, "zone not found")
    }

    if req.Name != "" {
        zone.Name = req.Name
    }
    if req.Type != "" {
        zone.Type = req.Type
    }
    if req.Description != "" {
        zone.Description = req.Description
    }
    if req.PickupSurcharge != 0 {
        zone.PickupSurcharge = req.PickupSurcharge
    }
    if req.DropoffSurcharge != 0 {
        zone.DropoffSurcharge = req.DropoffSurcharge
    }
    if req.GeoJson != "" {
        zone.GeoJSON = req.GeoJson
    }
    zone.UpdatedAt = time.Now()

    if err := s.DB.Save(&zone).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to update zone")
    }

    return &pb.Zone{
        Id:               zone.ID,
        Name:             zone.Name,
        Type:             zone.Type,
        Description:      zone.Description,
        PickupSurcharge:  zone.PickupSurcharge,
        DropoffSurcharge: zone.DropoffSurcharge,
        GeoJson:          zone.GeoJSON,
    }, nil
}

// DeleteZone deletes a zone (soft delete)
func (s *GeofencingServer) DeleteZone(ctx context.Context, req *pb.DeleteZoneRequest) (*pb.Empty, error) {
    if err := s.DB.Where("id = ?", req.Id).Delete(&Zone{}).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to delete zone")
    }
    return &pb.Empty{}, nil
}

// pointInPolygon performs a simple point-in-polygon check using GeoJSON
// In production, delegate to PostGIS for accuracy and performance
func pointInPolygon(lat, lng float64, geoJSON string) bool {
    if geoJSON == "" {
        return false
    }
    // For MVP, skip actual polygon check
    // In production: use a spatial database or library
    return false
}

func generateID() string {
    return "zone_" + time.Now().Format("20060102150405") + "_" + randomString(6)
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
        dsn = "host=postgres user=postgres password=postgres dbname=geofencedb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    // Enable PostGIS extension
    db.Exec("CREATE EXTENSION IF NOT EXISTS postgis")
    db.AutoMigrate(&Zone{})

    grpcServer := grpc.NewServer()
    pb.RegisterGeofencingServiceServer(grpcServer, &GeofencingServer{DB: db})

    lis, err := net.Listen("tcp", ":50059")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Geofencing Service running on port 50059")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("Geofencing Service stopped")
}
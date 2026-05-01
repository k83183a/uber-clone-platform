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

    pb "github.com/uber-clone/geofencing-service/proto"
)

type Zone struct {
    ID                string    `gorm:"primaryKey"`
    Name              string    `gorm:"uniqueIndex;not null"`
    Type              string    `gorm:"index"` // city, clean_air, fare_zone, surge_zone
    Description       string
    PickupSurcharge   float64   `gorm:"default:0"`
    DropoffSurcharge  float64   `gorm:"default:0"`
    GeoJSON           string    `gorm:"type:text"`
    IsActive          bool      `gorm:"default:true"`
    CreatedAt         time.Time
    UpdatedAt         time.Time
}

type GeofencingServer struct {
    pb.UnimplementedGeofencingServiceServer
    DB *gorm.DB
}

// PointInZone - Check if a point is inside a zone
func (s *GeofencingServer) PointInZone(ctx context.Context, req *pb.PointInZoneRequest) (*pb.PointInZoneResponse, error) {
    var zone Zone
    if err := s.DB.Where("name = ? AND is_active = ?", req.ZoneName, true).First(&zone).Error; err != nil {
        return nil, status.Error(codes.NotFound, "zone not found")
    }

    // In production: ST_Contains query with PostGIS
    var contains bool
    err := s.DB.Raw(`
        SELECT ST_Contains(
            geom, 
            ST_SetSRID(ST_MakePoint(?, ?), 4326)
        ) FROM zones WHERE name = ? AND is_active = ?
    `, req.Longitude, req.Latitude, req.ZoneName, true).Scan(&contains).Error
    if err != nil {
        contains = false
    }

    return &pb.PointInZoneResponse{
        Contains:         contains,
        PickupSurcharge:  zone.PickupSurcharge,
        DropoffSurcharge: zone.DropoffSurcharge,
    }, nil
}

// GetZonesForLocation - Get all zones containing a point
func (s *GeofencingServer) GetZonesForLocation(ctx context.Context, req *pb.GetZonesForLocationRequest) (*pb.GetZonesForLocationResponse, error) {
    var zones []Zone
    if err := s.DB.Where("is_active = ?", true).Find(&zones).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to fetch zones")
    }

    var matchedZones []*pb.ZoneInfo
    totalPickup := 0.0
    totalDropoff := 0.0

    for _, z := range zones {
        var contains bool
        s.DB.Raw(`
            SELECT ST_Contains(
                geom, 
                ST_SetSRID(ST_MakePoint(?, ?), 4326)
            ) FROM zones WHERE name = ? AND is_active = ?
        `, req.Longitude, req.Latitude, z.Name, true).Scan(&contains)

        if contains {
            matchedZones = append(matchedZones, &pb.ZoneInfo{
                ZoneName:         z.Name,
                ZoneType:         z.Type,
                PickupSurcharge:  z.PickupSurcharge,
                DropoffSurcharge: z.DropoffSurcharge,
            })
            totalPickup += z.PickupSurcharge
            totalDropoff += z.DropoffSurcharge
        }
    }

    return &pb.GetZonesForLocationResponse{
        Zones:                matchedZones,
        TotalPickupSurcharge: totalPickup,
        TotalDropoffSurcharge: totalDropoff,
    }, nil
}

// GetCity - Get city for a location
func (s *GeofencingServer) GetCity(ctx context.Context, req *pb.GetCityRequest) (*pb.GetCityResponse, error) {
    var zones []Zone
    s.DB.Where("type = ? AND is_active = ?", "city", true).Find(&zones)

    for _, z := range zones {
        var contains bool
        s.DB.Raw(`
            SELECT ST_Contains(
                geom, 
                ST_SetSRID(ST_MakePoint(?, ?), 4326)
            ) FROM zones WHERE name = ? AND type = 'city'
        `, req.Longitude, req.Latitude, z.Name).Scan(&contains)

        if contains {
            return &pb.GetCityResponse{City: z.Name}, nil
        }
    }

    return &pb.GetCityResponse{City: "unknown"}, nil
}

// ListZones - List all zones
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

// CreateZone - Create a new zone (admin)
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

    // Insert geometry into PostGIS
    s.DB.Exec(`
        INSERT INTO zones_geom (zone_id, geom)
        VALUES (?, ST_GeomFromGeoJSON(?))
        ON CONFLICT (zone_id) DO UPDATE SET geom = EXCLUDED.geom
    `, zone.ID, req.GeoJson)

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

// UpdateZone - Update an existing zone
func (s *GeofencingServer) UpdateZone(ctx context.Context, req *pb.UpdateZoneRequest) (*pb.Zone, error) {
    var zone Zone
    if err := s.DB.Where("id = ?", req.Id).First(&zone).Error; err != nil {
        return nil, status.Error(codes.NotFound, "zone not found")
    }

    if req.Name != "" { zone.Name = req.Name }
    if req.Type != "" { zone.Type = req.Type }
    if req.PickupSurcharge != 0 { zone.PickupSurcharge = req.PickupSurcharge }
    if req.DropoffSurcharge != 0 { zone.DropoffSurcharge = req.DropoffSurcharge }
    if req.GeoJson != "" {
        zone.GeoJSON = req.GeoJson
        s.DB.Exec(`UPDATE zones_geom SET geom = ST_GeomFromGeoJSON(?) WHERE zone_id = ?`, req.GeoJson, zone.ID)
    }
    zone.UpdatedAt = time.Now()
    s.DB.Save(&zone)

    return &pb.Zone{
        Id:               zone.ID,
        Name:             zone.Name,
        Type:             zone.Type,
        PickupSurcharge:  zone.PickupSurcharge,
        DropoffSurcharge: zone.DropoffSurcharge,
        GeoJson:          zone.GeoJSON,
    }, nil
}

// DeleteZone - Delete a zone
func (s *GeofencingServer) DeleteZone(ctx context.Context, req *pb.DeleteZoneRequest) (*pb.Empty, error) {
    s.DB.Where("id = ?", req.Id).Delete(&Zone{})
    s.DB.Exec("DELETE FROM zones_geom WHERE zone_id = ?", req.Id)
    return &pb.Empty{}, nil
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
        dsn = "host=postgres user=postgres password=postgres dbname=geodb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    // Enable PostGIS extension
    db.Exec("CREATE EXTENSION IF NOT EXISTS postgis")
    db.AutoMigrate(&Zone{})
    db.Exec(`CREATE TABLE IF NOT EXISTS zones_geom (zone_id TEXT PRIMARY KEY, geom GEOMETRY(Polygon, 4326))`)

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
}
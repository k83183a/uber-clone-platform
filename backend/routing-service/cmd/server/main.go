package main

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net"
    "net/http"
    "net/url"
    "os"
    "os/signal"
    "strconv"
    "strings"
    "syscall"
    "time"

    "github.com/joho/godotenv"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"

    pb "github.com/uber-clone/routing-service/proto"
)

type RoutingServer struct {
    pb.UnimplementedRoutingServiceServer
    googleAPIKey string
    mapboxAPIKey string
    osmURL       string
    httpClient   *http.Client
}

func NewRoutingServer(googleKey, mapboxKey, osmURL string) *RoutingServer {
    return &RoutingServer{
        googleAPIKey: googleKey,
        mapboxAPIKey: mapboxKey,
        osmURL:       osmURL,
        httpClient:   &http.Client{Timeout: 10 * time.Second},
    }
}

// GetDirections - Get directions from origin to destination
func (s *RoutingServer) GetDirections(ctx context.Context, req *pb.DirectionsRequest) (*pb.DirectionsResponse, error) {
    provider := req.Provider
    if provider == "" {
        provider = "google"
    }

    switch provider {
    case "google":
        return s.getGoogleDirections(req)
    case "mapbox":
        return s.getMapboxDirections(req)
    case "osm":
        return s.getOSMDirections(req)
    default:
        return nil, status.Error(codes.InvalidArgument, "unsupported provider")
    }
}

// Geocode - Convert address to coordinates
func (s *RoutingServer) Geocode(ctx context.Context, req *pb.GeocodeRequest) (*pb.GeocodeResponse, error) {
    provider := req.Provider
    if provider == "" {
        provider = "google"
    }

    switch provider {
    case "google":
        return s.googleGeocode(req.Query, int(req.Limit))
    case "mapbox":
        return s.mapboxGeocode(req.Query, int(req.Limit))
    case "osm":
        return s.osmGeocode(req.Query, int(req.Limit))
    default:
        return nil, status.Error(codes.InvalidArgument, "unsupported provider")
    }
}

// ReverseGeocode - Convert coordinates to address
func (s *RoutingServer) ReverseGeocode(ctx context.Context, req *pb.ReverseGeocodeRequest) (*pb.ReverseGeocodeResponse, error) {
    provider := req.Provider
    if provider == "" {
        provider = "google"
    }

    switch provider {
    case "google":
        return s.googleReverseGeocode(req.Location)
    case "mapbox":
        return s.mapboxReverseGeocode(req.Location)
    case "osm":
        return s.osmReverseGeocode(req.Location)
    default:
        return nil, status.Error(codes.InvalidArgument, "unsupported provider")
    }
}

// GetDistanceMatrix - Get distance matrix (Google only)
func (s *RoutingServer) GetDistanceMatrix(ctx context.Context, req *pb.DistanceMatrixRequest) (*pb.DistanceMatrixResponse, error) {
    return s.googleDistanceMatrix(req)
}

func (s *RoutingServer) getGoogleDirections(req *pb.DirectionsRequest) (*pb.DirectionsResponse, error) {
    if s.googleAPIKey == "" {
        return nil, status.Error(codes.Unavailable, "Google Maps API key not configured")
    }

    origin := fmt.Sprintf("%f,%f", req.Origin.Lat, req.Origin.Lng)
    dest := fmt.Sprintf("%f,%f", req.Destination.Lat, req.Destination.Lng)
    apiURL := fmt.Sprintf("https://maps.googleapis.com/maps/api/directions/json?origin=%s&destination=%s&mode=driving&key=%s", origin, dest, s.googleAPIKey)

    resp, err := s.httpClient.Get(apiURL)
    if err != nil {
        return nil, status.Error(codes.Unavailable, "failed to call Google Directions API")
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)
    var result map[string]interface{}
    json.Unmarshal(body, &result)

    return s.parseGoogleDirections(result), nil
}

func (s *RoutingServer) parseGoogleDirections(result map[string]interface{}) *pb.DirectionsResponse {
    routes := []*pb.Route{}
    if routesData, ok := result["routes"].([]interface{}); ok {
        for _, r := range routesData {
            route := r.(map[string]interface{})
            legs := route["legs"].([]interface{})
            if len(legs) == 0 { continue }
            leg := legs[0].(map[string]interface{})

            distance := 0.0
            if dist, ok := leg["distance"].(map[string]interface{}); ok {
                distance = dist["value"].(float64) / 1000
            }
            duration := 0
            if dur, ok := leg["duration"].(map[string]interface{}); ok {
                duration = int(dur["value"].(float64))
            }
            polyline := ""
            if overviewPoly, ok := route["overview_polyline"].(map[string]interface{}); ok {
                polyline = overviewPoly["points"].(string)
            }

            routes = append(routes, &pb.Route{
                Polyline:        polyline,
                DistanceMeters:  distance * 1000,
                DurationSeconds: int32(duration),
            })
        }
    }
    return &pb.DirectionsResponse{Routes: routes, ProviderUsed: "google"}
}

func (s *RoutingServer) getMapboxDirections(req *pb.DirectionsRequest) (*pb.DirectionsResponse, error) {
    if s.mapboxAPIKey == "" {
        return nil, status.Error(codes.Unavailable, "Mapbox API key not configured")
    }

    origin := fmt.Sprintf("%f,%f", req.Origin.Lng, req.Origin.Lat)
    dest := fmt.Sprintf("%f,%f", req.Destination.Lng, req.Destination.Lat)
    apiURL := fmt.Sprintf("https://api.mapbox.com/directions/v5/mapbox/driving/%s;%s?geometries=polyline&overview=full&access_token=%s", origin, dest, s.mapboxAPIKey)

    resp, err := s.httpClient.Get(apiURL)
    if err != nil {
        return nil, status.Error(codes.Unavailable, "failed to call Mapbox Directions API")
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)
    var result map[string]interface{}
    json.Unmarshal(body, &result)

    return s.parseMapboxDirections(result), nil
}

func (s *RoutingServer) parseMapboxDirections(result map[string]interface{}) *pb.DirectionsResponse {
    routes := []*pb.Route{}
    if routesData, ok := result["routes"].([]interface{}); ok {
        for _, r := range routesData {
            route := r.(map[string]interface{})
            distance := route["distance"].(float64) / 1000
            duration := route["duration"].(float64)
            geometry := route["geometry"].(string)
            routes = append(routes, &pb.Route{
                Polyline:        geometry,
                DistanceMeters:  distance * 1000,
                DurationSeconds: int32(duration),
            })
        }
    }
    return &pb.DirectionsResponse{Routes: routes, ProviderUsed: "mapbox"}
}

func (s *RoutingServer) getOSMDirections(req *pb.DirectionsRequest) (*pb.DirectionsResponse, error) {
    origin := fmt.Sprintf("%f,%f", req.Origin.Lng, req.Origin.Lat)
    dest := fmt.Sprintf("%f,%f", req.Destination.Lng, req.Destination.Lat)
    apiURL := fmt.Sprintf("%s/route/v1/driving/%s;%s?overview=full&geometries=polyline", s.osmURL, origin, dest)

    resp, err := s.httpClient.Get(apiURL)
    if err != nil {
        return nil, status.Error(codes.Unavailable, "failed to call OSRM API")
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)
    var result map[string]interface{}
    json.Unmarshal(body, &result)

    return s.parseOSMDirections(result), nil
}

func (s *RoutingServer) parseOSMDirections(result map[string]interface{}) *pb.DirectionsResponse {
    routes := []*pb.Route{}
    if routesData, ok := result["routes"].([]interface{}); ok {
        for _, r := range routesData {
            route := r.(map[string]interface{})
            distance := route["distance"].(float64) / 1000
            duration := route["duration"].(float64)
            geometry := route["geometry"].(string)
            routes = append(routes, &pb.Route{
                Polyline:        geometry,
                DistanceMeters:  distance * 1000,
                DurationSeconds: int32(duration),
            })
        }
    }
    return &pb.DirectionsResponse{Routes: routes, ProviderUsed: "osm"}
}

func (s *RoutingServer) googleGeocode(query string, limit int) (*pb.GeocodeResponse, error) {
    apiURL := fmt.Sprintf("https://maps.googleapis.com/maps/api/geocode/json?address=%s&key=%s", url.QueryEscape(query), s.googleAPIKey)
    resp, err := s.httpClient.Get(apiURL)
    if err != nil {
        return nil, status.Error(codes.Unavailable, "failed to call Google Geocoding API")
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)
    var result map[string]interface{}
    json.Unmarshal(body, &result)

    results := []*pb.GeocodeResult{}
    if resultsData, ok := result["results"].([]interface{}); ok {
        for i, res := range resultsData {
            if limit > 0 && i >= limit { break }
            r := res.(map[string]interface{})
            formattedAddress := r["formatted_address"].(string)
            geometry := r["geometry"].(map[string]interface{})
            location := geometry["location"].(map[string]interface{})
            results = append(results, &pb.GeocodeResult{
                FormattedAddress: formattedAddress,
                Location:         &pb.Location{Lat: location["lat"].(float64), Lng: location["lng"].(float64)},
                Confidence:       1.0 - (float64(i) * 0.1),
            })
        }
    }
    return &pb.GeocodeResponse{Results: results}, nil
}

func (s *RoutingServer) mapboxGeocode(query string, limit int) (*pb.GeocodeResponse, error) {
    apiURL := fmt.Sprintf("https://api.mapbox.com/geocoding/v5/mapbox.places/%s.json?access_token=%s&limit=%d", url.QueryEscape(query), s.mapboxAPIKey, limit)
    resp, err := s.httpClient.Get(apiURL)
    if err != nil {
        return nil, status.Error(codes.Unavailable, "failed to call Mapbox Geocoding API")
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)
    var result map[string]interface{}
    json.Unmarshal(body, &result)

    results := []*pb.GeocodeResult{}
    if features, ok := result["features"].([]interface{}); ok {
        for i, feat := range features {
            if limit > 0 && i >= limit { break }
            f := feat.(map[string]interface{})
            placeName := f["place_name"].(string)
            center := f["center"].([]interface{})
            results = append(results, &pb.GeocodeResult{
                FormattedAddress: placeName,
                Location:         &pb.Location{Lat: center[1].(float64), Lng: center[0].(float64)},
                Confidence:       0.9,
            })
        }
    }
    return &pb.GeocodeResponse{Results: results}, nil
}

func (s *RoutingServer) osmGeocode(query string, limit int) (*pb.GeocodeResponse, error) {
    apiURL := fmt.Sprintf("https://nominatim.openstreetmap.org/search?q=%s&format=json&limit=%d", url.QueryEscape(query), limit)
    resp, err := s.httpClient.Get(apiURL)
    if err != nil {
        return nil, status.Error(codes.Unavailable, "failed to call Nominatim API")
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)
    var resultsData []map[string]interface{}
    json.Unmarshal(body, &resultsData)

    results := []*pb.GeocodeResult{}
    for i, r := range resultsData {
        if limit > 0 && i >= limit { break }
        lat, _ := strconv.ParseFloat(r["lat"].(string), 64)
        lon, _ := strconv.ParseFloat(r["lon"].(string), 64)
        results = append(results, &pb.GeocodeResult{
            FormattedAddress: r["display_name"].(string),
            Location:         &pb.Location{Lat: lat, Lng: lon},
            Confidence:       0.8,
        })
    }
    return &pb.GeocodeResponse{Results: results}, nil
}

func (s *RoutingServer) googleReverseGeocode(loc *pb.Location) (*pb.ReverseGeocodeResponse, error) {
    apiURL := fmt.Sprintf("https://maps.googleapis.com/maps/api/geocode/json?latlng=%f,%f&key=%s", loc.Lat, loc.Lng, s.googleAPIKey)
    resp, err := s.httpClient.Get(apiURL)
    if err != nil {
        return nil, status.Error(codes.Unavailable, "failed to call Google Geocoding API")
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)
    var result map[string]interface{}
    json.Unmarshal(body, &result)

    results := []*pb.GeocodeResult{}
    if resultsData, ok := result["results"].([]interface{}); ok && len(resultsData) > 0 {
        r := resultsData[0].(map[string]interface{})
        results = append(results, &pb.GeocodeResult{
            FormattedAddress: r["formatted_address"].(string),
            Location:         loc,
            Confidence:       0.9,
        })
    }
    return &pb.ReverseGeocodeResponse{Results: results}, nil
}

func (s *RoutingServer) mapboxReverseGeocode(loc *pb.Location) (*pb.ReverseGeocodeResponse, error) {
    apiURL := fmt.Sprintf("https://api.mapbox.com/geocoding/v5/mapbox.places/%f,%f.json?access_token=%s", loc.Lng, loc.Lat, s.mapboxAPIKey)
    resp, err := s.httpClient.Get(apiURL)
    if err != nil {
        return nil, status.Error(codes.Unavailable, "failed to call Mapbox Geocoding API")
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)
    var result map[string]interface{}
    json.Unmarshal(body, &result)

    results := []*pb.GeocodeResult{}
    if features, ok := result["features"].([]interface{}); ok && len(features) > 0 {
        f := features[0].(map[string]interface{})
        results = append(results, &pb.GeocodeResult{
            FormattedAddress: f["place_name"].(string),
            Location:         loc,
            Confidence:       0.9,
        })
    }
    return &pb.ReverseGeocodeResponse{Results: results}, nil
}

func (s *RoutingServer) osmReverseGeocode(loc *pb.Location) (*pb.ReverseGeocodeResponse, error) {
    apiURL := fmt.Sprintf("https://nominatim.openstreetmap.org/reverse?format=json&lat=%f&lon=%f", loc.Lat, loc.Lng)
    resp, err := s.httpClient.Get(apiURL)
    if err != nil {
        return nil, status.Error(codes.Unavailable, "failed to call Nominatim API")
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)
    var result map[string]interface{}
    json.Unmarshal(body, &result)

    results := []*pb.GeocodeResult{}
    if displayName, ok := result["display_name"].(string); ok {
        results = append(results, &pb.GeocodeResult{
            FormattedAddress: displayName,
            Location:         loc,
            Confidence:       0.8,
        })
    }
    return &pb.ReverseGeocodeResponse{Results: results}, nil
}

func (s *RoutingServer) googleDistanceMatrix(req *pb.DistanceMatrixRequest) (*pb.DistanceMatrixResponse, error) {
    origins := []string{}
    for _, o := range req.Origins {
        origins = append(origins, fmt.Sprintf("%f,%f", o.Lat, o.Lng))
    }
    destinations := []string{}
    for _, d := range req.Destinations {
        destinations = append(destinations, fmt.Sprintf("%f,%f", d.Lat, d.Lng))
    }

    apiURL := fmt.Sprintf("https://maps.googleapis.com/maps/api/distancematrix/json?origins=%s&destinations=%s&key=%s", strings.Join(origins, "|"), strings.Join(destinations, "|"), s.googleAPIKey)
    resp, err := s.httpClient.Get(apiURL)
    if err != nil {
        return nil, status.Error(codes.Unavailable, "failed to call Google Distance Matrix API")
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)
    var result map[string]interface{}
    json.Unmarshal(body, &result)

    rows := []*pb.Row{}
    if rowsData, ok := result["rows"].([]interface{}); ok {
        for _, r := range rowsData {
            row := r.(map[string]interface{})
            elements := []*pb.Element{}
            if elementsData, ok := row["elements"].([]interface{}); ok {
                for _, e := range elementsData {
                    elem := e.(map[string]interface{})
                    distanceVal := 0.0
                    durationVal := 0
                    status := elem["status"].(string)
                    if dist, ok := elem["distance"].(map[string]interface{}); ok {
                        distanceVal = dist["value"].(float64) / 1000
                    }
                    if dur, ok := elem["duration"].(map[string]interface{}); ok {
                        durationVal = int(dur["value"].(float64))
                    }
                    elements = append(elements, &pb.Element{
                        DistanceMeters:  distanceVal * 1000,
                        DurationSeconds: int32(durationVal),
                        Status:          status,
                    })
                }
            }
            rows = append(rows, &pb.Row{Elements: elements})
        }
    }
    return &pb.DistanceMatrixResponse{Rows: rows}, nil
}

func (s *RoutingServer) GetMapTile(ctx context.Context, req *pb.MapTileRequest) (*pb.MapTileResponse, error) {
    return nil, status.Error(codes.Unimplemented, "map tile endpoint not implemented")
}

func main() {
    godotenv.Load()

    googleKey := os.Getenv("GOOGLE_MAPS_API_KEY")
    mapboxKey := os.Getenv("MAPBOX_API_KEY")
    osmURL := os.Getenv("OSRM_BASE_URL")
    if osmURL == "" {
        osmURL = "https://router.project-osrm.org"
    }

    server := NewRoutingServer(googleKey, mapboxKey, osmURL)

    grpcServer := grpc.NewServer()
    pb.RegisterRoutingServiceServer(grpcServer, server)

    lis, err := net.Listen("tcp", ":50078")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Routing Service running on port 50078")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
}
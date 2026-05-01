package main

import (
    "context"
    "encoding/json"
    "log"
    "net"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/gorilla/websocket"
    "github.com/joho/godotenv"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"

    pb "github.com/uber-clone/bid-service/proto"
)

// BidRequest represents a ride bidding request from a rider
type BidRequest struct {
    ID             string    `gorm:"primaryKey"`
    RiderID        string    `gorm:"index;not null"`
    PickupLat      float64   `gorm:"not null"`
    PickupLng      float64   `gorm:"not null"`
    PickupAddress  string
    DropoffLat     float64   `gorm:"not null"`
    DropoffLng     float64   `gorm:"not null"`
    DropoffAddress string
    Status         string    `gorm:"default:'open'"` // open, expired, accepted
    ExpiresAt      time.Time `gorm:"not null"`
    CreatedAt      time.Time
    UpdatedAt      time.Time
}

// DriverBid represents a bid submitted by a driver
type DriverBid struct {
    ID         string    `gorm:"primaryKey"`
    RequestID  string    `gorm:"index;not null"`
    DriverID   string    `gorm:"index;not null"`
    BidAmount  float64   `gorm:"not null"`
    Status     string    `gorm:"default:'active'"` // active, accepted, rejected
    BidTime    time.Time
    CreatedAt  time.Time
}

// BidWebSocketHub manages live bid updates
type BidWebSocketHub struct {
    clients    map[string]map[*websocket.Conn]bool // requestID -> connections
    clientsMu  sync.RWMutex
    register   chan *bidSubscription
    unregister chan *bidSubscription
    broadcast  chan *DriverBid
    upgrader   websocket.Upgrader
}

type bidSubscription struct {
    RequestID string
    Conn      *websocket.Conn
}

// BidServer handles gRPC requests
type BidServer struct {
    pb.UnimplementedBidServiceServer
    DB  *gorm.DB
    hub *BidWebSocketHub
}

// NewBidWebSocketHub creates a new hub
func NewBidWebSocketHub() *BidWebSocketHub {
    return &BidWebSocketHub{
        clients:    make(map[string]map[*websocket.Conn]bool),
        register:   make(chan *bidSubscription),
        unregister: make(chan *bidSubscription),
        broadcast:  make(chan *DriverBid, 256),
        upgrader: websocket.Upgrader{
            CheckOrigin: func(r *http.Request) bool { return true },
        },
    }
}

// Run starts the WebSocket hub
func (h *BidWebSocketHub) Run() {
    for {
        select {
        case sub := <-h.register:
            h.clientsMu.Lock()
            if _, ok := h.clients[sub.RequestID]; !ok {
                h.clients[sub.RequestID] = make(map[*websocket.Conn]bool)
            }
            h.clients[sub.RequestID][sub.Conn] = true
            h.clientsMu.Unlock()

        case sub := <-h.unregister:
            h.clientsMu.Lock()
            if clients, ok := h.clients[sub.RequestID]; ok {
                delete(clients, sub.Conn)
                if len(clients) == 0 {
                    delete(h.clients, sub.RequestID)
                }
            }
            h.clientsMu.Unlock()
            sub.Conn.Close()

        case bid := <-h.broadcast:
            h.clientsMu.RLock()
            clients := h.clients[bid.RequestID]
            h.clientsMu.RUnlock()

            msg, _ := json.Marshal(bid)
            for conn := range clients {
                conn.WriteMessage(websocket.TextMessage, msg)
            }
        }
    }
}

// HandleWebSocket upgrades HTTP to WebSocket for live bidding
func (h *BidWebSocketHub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
    requestID := r.URL.Query().Get("request_id")
    if requestID == "" {
        http.Error(w, "request_id required", http.StatusBadRequest)
        return
    }

    conn, err := h.upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Printf("WebSocket upgrade error: %v", err)
        return
    }

    sub := &bidSubscription{
        RequestID: requestID,
        Conn:      conn,
    }
    h.register <- sub
    defer func() { h.unregister <- sub }()

    // Keep connection alive
    for {
        _, _, err := conn.ReadMessage()
        if err != nil {
            break
        }
    }
}

// CreateBidRequest creates a new ride bidding request
func (s *BidServer) CreateBidRequest(ctx context.Context, req *pb.CreateBidRequestRequest) (*pb.BidRequestResponse, error) {
    expiresAt := time.Now().Add(time.Duration(req.ExpiresInSeconds) * time.Second)

    bidReq := &BidRequest{
        ID:             generateID(),
        RiderID:        req.RiderId,
        PickupLat:      req.PickupLat,
        PickupLng:      req.PickupLng,
        PickupAddress:  req.PickupAddress,
        DropoffLat:     req.DropoffLat,
        DropoffLng:     req.DropoffLng,
        DropoffAddress: req.DropoffAddress,
        Status:         "open",
        ExpiresAt:      expiresAt,
        CreatedAt:      time.Now(),
        UpdatedAt:      time.Now(),
    }

    if err := s.DB.Create(bidReq).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create bid request")
    }

    // In production: find nearest 10 drivers via dispatch service and send push notifications
    // For MVP, we'll rely on drivers manually checking open bids

    return &pb.BidRequestResponse{
        RequestId: bidReq.ID,
        Status:    bidReq.Status,
        ExpiresAt: bidReq.ExpiresAt.Unix(),
    }, nil
}

// SubmitBid submits a driver's bid for a ride request
func (s *BidServer) SubmitBid(ctx context.Context, req *pb.SubmitBidRequest) (*pb.Empty, error) {
    // Check if bid request is still open
    var bidReq BidRequest
    if err := s.DB.Where("id = ? AND status = ?", req.RequestId, "open").First(&bidReq).Error; err != nil {
        return nil, status.Error(codes.NotFound, "bid request not found or expired")
    }

    if time.Now().After(bidReq.ExpiresAt) {
        bidReq.Status = "expired"
        s.DB.Save(&bidReq)
        return nil, status.Error(codes.FailedPrecondition, "bid request expired")
    }

    // Check if driver already submitted a bid
    var existing DriverBid
    if err := s.DB.Where("request_id = ? AND driver_id = ?", req.RequestId, req.DriverId).First(&existing).Error; err == nil {
        return nil, status.Error(codes.AlreadyExists, "driver already submitted a bid")
    }

    bid := &DriverBid{
        ID:        generateID(),
        RequestID: req.RequestId,
        DriverID:  req.DriverId,
        BidAmount: req.BidAmount,
        Status:    "active",
        BidTime:   time.Now(),
        CreatedAt: time.Now(),
    }

    if err := s.DB.Create(bid).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to submit bid")
    }

    // Broadcast to all riders subscribed to this request via WebSocket
    s.hub.broadcast <- bid

    return &pb.Empty{}, nil
}

// GetBidsForRequest returns all bids for a ride request
func (s *BidServer) GetBidsForRequest(ctx context.Context, req *pb.GetBidsRequest) (*pb.BidsList, error) {
    var bids []DriverBid
    if err := s.DB.Where("request_id = ? AND status = ?", req.RequestId, "active").Order("bid_amount ASC").Find(&bids).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to get bids")
    }

    var pbBids []*pb.DriverBid
    for _, b := range bids {
        pbBids = append(pbBids, &pb.DriverBid{
            BidId:     b.ID,
            DriverId:  b.DriverID,
            BidAmount: b.BidAmount,
            BidTime:   b.BidTime.Unix(),
        })
    }

    return &pb.BidsList{Bids: pbBids}, nil
}

// AcceptBid accepts a driver's bid and creates a ride
func (s *BidServer) AcceptBid(ctx context.Context, req *pb.AcceptBidRequest) (*pb.Empty, error) {
    // Get the bid request
    var bidReq BidRequest
    if err := s.DB.Where("id = ?", req.RequestId).First(&bidReq).Error; err != nil {
        return nil, status.Error(codes.NotFound, "bid request not found")
    }

    if bidReq.Status != "open" {
        return nil, status.Error(codes.FailedPrecondition, "bid request already processed")
    }

    if time.Now().After(bidReq.ExpiresAt) {
        bidReq.Status = "expired"
        s.DB.Save(&bidReq)
        return nil, status.Error(codes.FailedPrecondition, "bid request expired")
    }

    // Get the bid
    var bid DriverBid
    if err := s.DB.Where("id = ? AND request_id = ?", req.BidId, req.RequestId).First(&bid).Error; err != nil {
        return nil, status.Error(codes.NotFound, "bid not found")
    }

    if bid.Status != "active" {
        return nil, status.Error(codes.FailedPrecondition, "bid already processed")
    }

    // Mark bid as accepted
    bid.Status = "accepted"
    s.DB.Save(&bid)

    // Mark all other bids as rejected
    s.DB.Model(&DriverBid{}).Where("request_id = ? AND id != ?", req.RequestId, req.BidId).Update("status", "rejected")

    // Mark bid request as accepted
    bidReq.Status = "accepted"
    s.DB.Save(&bidReq)

    // In production: call ride-service to create a ride with the accepted driver and bid amount
    // For now, we'll just log
    log.Printf("✅ Bid accepted: Ride created for request %s, driver %s, amount £%.2f", req.RequestId, bid.DriverID, bid.BidAmount)

    return &pb.Empty{}, nil
}

// GetOpenBidRequests returns open bid requests (for drivers to view)
func (s *BidServer) GetOpenBidRequests(ctx context.Context, req *pb.GetOpenBidRequestsRequest) (*pb.OpenBidRequestsResponse, error) {
    var bidReqs []BidRequest
    if err := s.DB.Where("status = ? AND expires_at > ?", "open", time.Now()).Order("created_at ASC").Find(&bidReqs).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to get open bid requests")
    }

    var pbRequests []*pb.BidRequestSummary
    for _, br := range bidReqs {
        pbRequests = append(pbRequests, &pb.BidRequestSummary{
            RequestId:     br.ID,
            PickupLat:     br.PickupLat,
            PickupLng:     br.PickupLng,
            PickupAddress: br.PickupAddress,
            DropoffLat:    br.DropoffLat,
            DropoffLng:    br.DropoffLng,
            DropoffAddress: br.DropoffAddress,
            ExpiresAt:     br.ExpiresAt.Unix(),
        })
    }

    return &pb.OpenBidRequestsResponse{Requests: pbRequests}, nil
}

// GetBidRequestDetails returns details of a specific bid request
func (s *BidServer) GetBidRequestDetails(ctx context.Context, req *pb.GetBidRequestDetailsRequest) (*pb.BidRequestDetailsResponse, error) {
    var bidReq BidRequest
    if err := s.DB.Where("id = ?", req.RequestId).First(&bidReq).Error; err != nil {
        return nil, status.Error(codes.NotFound, "bid request not found")
    }

    return &pb.BidRequestDetailsResponse{
        RequestId:     bidReq.ID,
        RiderId:       bidReq.RiderID,
        PickupLat:     bidReq.PickupLat,
        PickupLng:     bidReq.PickupLng,
        PickupAddress: bidReq.PickupAddress,
        DropoffLat:    bidReq.DropoffLat,
        DropoffLng:    bidReq.DropoffLng,
        DropoffAddress: bidReq.DropoffAddress,
        Status:        bidReq.Status,
        ExpiresAt:     bidReq.ExpiresAt.Unix(),
    }, nil
}

func generateID() string {
    return "bid_" + time.Now().Format("20060102150405") + "_" + randomString(6)
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
        dsn = "host=postgres user=postgres password=postgres dbname=biddb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&BidRequest{}, &DriverBid{})

    hub := NewBidWebSocketHub()
    go hub.Run()

    // HTTP WebSocket endpoint for live bids
    http.HandleFunc("/ws/bids", hub.HandleWebSocket)
    go func() {
        log.Println("✅ Bid Service WebSocket running on port 8087")
        log.Fatal(http.ListenAndServe(":8087", nil))
    }()

    grpcServer := grpc.NewServer()
    pb.RegisterBidServiceServer(grpcServer, &BidServer{DB: db, hub: hub})

    lis, err := net.Listen("tcp", ":50081")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Bid Service gRPC running on port 50081")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("Bid Service stopped")
}
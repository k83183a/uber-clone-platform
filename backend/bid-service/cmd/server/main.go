package main

import (
    "context"
    "encoding/json"
    "log"
    "net"
    "net/http"
    "os"
    "os/signal"
    "sync"
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

type BidRequest struct {
    ID             string    `gorm:"primaryKey"`
    RiderID        string    `gorm:"index;not null"`
    PickupLat      float64   `gorm:"not null"`
    PickupLng      float64   `gorm:"not null"`
    PickupAddress  string
    DropoffLat     float64   `gorm:"not null"`
    DropoffLng     float64   `gorm:"not null"`
    DropoffAddress string
    Status         string    `gorm:"default:'open'"`
    ExpiresAt      time.Time `gorm:"not null"`
    CreatedAt      time.Time
    UpdatedAt      time.Time
}

type DriverBid struct {
    ID        string    `gorm:"primaryKey"`
    RequestID string    `gorm:"index;not null"`
    DriverID  string    `gorm:"index;not null"`
    BidAmount float64   `gorm:"not null"`
    Status    string    `gorm:"default:'active'"`
    BidTime   time.Time
    CreatedAt time.Time
}

type BidWebSocketHub struct {
    clients    map[string]map[*websocket.Conn]bool
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

type BidServer struct {
    pb.UnimplementedBidServiceServer
    DB  *gorm.DB
    hub *BidWebSocketHub
}

func NewBidWebSocketHub() *BidWebSocketHub {
    return &BidWebSocketHub{
        clients:    make(map[string]map[*websocket.Conn]bool),
        register:   make(chan *bidSubscription),
        unregister: make(chan *bidSubscription),
        broadcast:  make(chan *DriverBid, 256),
        upgrader:   websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
    }
}

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

func (h *BidWebSocketHub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
    requestID := r.URL.Query().Get("request_id")
    if requestID == "" {
        http.Error(w, "request_id required", http.StatusBadRequest)
        return
    }

    conn, err := h.upgrader.Upgrade(w, r, nil)
    if err != nil {
        return
    }

    sub := &bidSubscription{RequestID: requestID, Conn: conn}
    h.register <- sub
    defer func() { h.unregister <- sub }()

    for {
        _, _, err := conn.ReadMessage()
        if err != nil {
            break
        }
    }
}

// CreateBidRequest - Create bid request
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
    return &pb.BidRequestResponse{RequestId: bidReq.ID, Status: bidReq.Status, ExpiresAt: bidReq.ExpiresAt.Unix()}, nil
}

// SubmitBid - Driver submits bid
func (s *BidServer) SubmitBid(ctx context.Context, req *pb.SubmitBidRequest) (*pb.Empty, error) {
    var bidReq BidRequest
    if err := s.DB.Where("id = ? AND status = ?", req.RequestId, "open").First(&bidReq).Error; err != nil {
        return nil, status.Error(codes.NotFound, "bid request not found or expired")
    }
    if time.Now().After(bidReq.ExpiresAt) {
        bidReq.Status = "expired"
        s.DB.Save(&bidReq)
        return nil, status.Error(codes.FailedPrecondition, "bid request expired")
    }

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
    s.DB.Create(bid)
    s.hub.broadcast <- bid
    return &pb.Empty{}, nil
}

// GetBidsForRequest - Get all bids for request
func (s *BidServer) GetBidsForRequest(ctx context.Context, req *pb.GetBidsRequest) (*pb.BidsList, error) {
    var bids []DriverBid
    s.DB.Where("request_id = ? AND status = ?", req.RequestId, "active").Order("bid_amount ASC").Find(&bids)

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

// AcceptBid - Accept a bid
func (s *BidServer) AcceptBid(ctx context.Context, req *pb.AcceptBidRequest) (*pb.Empty, error) {
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

    var bid DriverBid
    if err := s.DB.Where("id = ? AND request_id = ?", req.BidId, req.RequestId).First(&bid).Error; err != nil {
        return nil, status.Error(codes.NotFound, "bid not found")
    }
    if bid.Status != "active" {
        return nil, status.Error(codes.FailedPrecondition, "bid already processed")
    }

    bid.Status = "accepted"
    s.DB.Save(&bid)
    s.DB.Model(&DriverBid{}).Where("request_id = ? AND id != ?", req.RequestId, req.BidId).Update("status", "rejected")
    bidReq.Status = "accepted"
    s.DB.Save(&bidReq)

    log.Printf("✅ Bid accepted: Ride created for request %s, driver %s, amount £%.2f", req.RequestId, bid.DriverID, bid.BidAmount)
    return &pb.Empty{}, nil
}

// GetOpenBidRequests - Get open bid requests
func (s *BidServer) GetOpenBidRequests(ctx context.Context, req *pb.GetOpenBidRequestsRequest) (*pb.OpenBidRequestsResponse, error) {
    var bidReqs []BidRequest
    s.DB.Where("status = ? AND expires_at > ?", "open", time.Now()).Order("created_at ASC").Find(&bidReqs)

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

// GetBidRequestDetails - Get bid request details
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

    http.HandleFunc("/ws/bids", hub.HandleWebSocket)
    go func() {
        log.Println("✅ Bid Service WebSocket on :8087")
        log.Fatal(http.ListenAndServe(":8087", nil))
    }()

    grpcServer := grpc.NewServer()
    pb.RegisterBidServiceServer(grpcServer, &BidServer{DB: db, hub: hub})

    lis, err := net.Listen("tcp", ":50081")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Bid Service gRPC on :50081")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
}
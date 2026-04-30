package main

import (
    "encoding/json"
    "log"
    "net/http"
    "os"
    "os/signal"
    "sync"
    "syscall"
    "time"

    "github.com/gorilla/websocket"
    "github.com/joho/godotenv"
)

// WebSocket upgrader
var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true // Allow all origins for production
    },
    ReadBufferSize:  1024,
    WriteBufferSize: 1024,
}

// Location update structure
type LocationUpdate struct {
    DriverID  string    `json:"driver_id"`
    Latitude  float64   `json:"lat"`
    Longitude float64   `json:"lng"`
    RideID    string    `json:"ride_id,omitempty"`
    Timestamp time.Time `json:"timestamp"`
}

// Driver client connection
type DriverClient struct {
    ID       string
    Conn     *websocket.Conn
    Send     chan []byte
    LastSeen time.Time
}

// Rider subscription
type RiderSubscription struct {
    RideID string
    Conn   *websocket.Conn
    Send   chan []byte
}

// Location hub manages all connections
type LocationHub struct {
    // Registered drivers
    drivers map[string]*DriverClient
    driversMu sync.RWMutex

    // Registered riders (by ride ID)
    riders map[string][]*RiderSubscription
    ridersMu sync.RWMutex

    // Broadcast channels
    registerDriver   chan *DriverClient
    unregisterDriver chan *DriverClient
    registerRider    chan *RiderSubscription
    unregisterRider  chan *RiderSubscription
    broadcast        chan LocationUpdate
}

// Create new hub
func newLocationHub() *LocationHub {
    return &LocationHub{
        drivers:          make(map[string]*DriverClient),
        riders:           make(map[string][]*RiderSubscription),
        registerDriver:   make(chan *DriverClient),
        unregisterDriver: make(chan *DriverClient),
        registerRider:    make(chan *RiderSubscription),
        unregisterRider:  make(chan *RiderSubscription),
        broadcast:        make(chan LocationUpdate, 256),
    }
}

// Run the hub
func (h *LocationHub) run() {
    for {
        select {
        case client := <-h.registerDriver:
            h.driversMu.Lock()
            // Close existing connection if any
            if old, ok := h.drivers[client.ID]; ok {
                old.Conn.Close()
            }
            h.drivers[client.ID] = client
            h.driversMu.Unlock()
            log.Printf("✅ Driver %s connected. Total drivers: %d", client.ID, len(h.drivers))

        case client := <-h.unregisterDriver:
            h.driversMu.Lock()
            if _, ok := h.drivers[client.ID]; ok {
                delete(h.drivers, client.ID)
                close(client.Send)
                log.Printf("❌ Driver %s disconnected. Total drivers: %d", client.ID, len(h.drivers))
            }
            h.driversMu.Unlock()

        case sub := <-h.registerRider:
            h.ridersMu.Lock()
            h.riders[sub.RideID] = append(h.riders[sub.RideID], sub)
            h.ridersMu.Unlock()
            log.Printf("✅ Rider subscribed to ride %s", sub.RideID)

        case sub := <-h.unregisterRider:
            h.ridersMu.Lock()
            if riders, ok := h.riders[sub.RideID]; ok {
                for i, r := range riders {
                    if r == sub {
                        riders = append(riders[:i], riders[i+1:]...)
                        break
                    }
                }
                if len(riders) == 0 {
                    delete(h.riders, sub.RideID)
                } else {
                    h.riders[sub.RideID] = riders
                }
                close(sub.Send)
            }
            h.ridersMu.Unlock()
            log.Printf("❌ Rider unsubscribed from ride %s", sub.RideID)

        case update := <-h.broadcast:
            h.driversMu.RLock()
            // Store last location locally for driver (could also store in Redis/MongoDB)
            // For now, just broadcast to riders
            h.driversMu.RUnlock()

            // Broadcast to riders subscribed to this ride
            if update.RideID != "" {
                h.ridersMu.RLock()
                if subs, ok := h.riders[update.RideID]; ok {
                    msg, _ := json.Marshal(update)
                    for _, sub := range subs {
                        select {
                        case sub.Send <- msg:
                        default:
                            // Client blocked, will be cleaned up
                        }
                    }
                }
                h.ridersMu.RUnlock()
            }
        }
    }
}

// Driver WebSocket handler
func (h *LocationHub) handleDriverWS(w http.ResponseWriter, r *http.Request) {
    driverID := r.URL.Query().Get("driver_id")
    if driverID == "" {
        http.Error(w, "driver_id required", http.StatusBadRequest)
        return
    }

    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Printf("Driver WS upgrade error: %v", err)
        return
    }

    client := &DriverClient{
        ID:       driverID,
        Conn:     conn,
        Send:     make(chan []byte, 256),
        LastSeen: time.Now(),
    }

    h.registerDriver <- client

    // Start write pump
    go client.writePump()

    // Read pump
    client.readPump(h)
}

// Rider WebSocket handler
func (h *LocationHub) handleRiderWS(w http.ResponseWriter, r *http.Request) {
    rideID := r.URL.Query().Get("ride_id")
    if rideID == "" {
        http.Error(w, "ride_id required", http.StatusBadRequest)
        return
    }

    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Printf("Rider WS upgrade error: %v", err)
        return
    }

    sub := &RiderSubscription{
        RideID: rideID,
        Conn:   conn,
        Send:   make(chan []byte, 256),
    }

    h.registerRider <- sub

    // Start write pump
    go sub.writePump()

    // Read pump (just wait for close)
    for {
        if _, _, err := conn.ReadMessage(); err != nil {
            break
        }
    }

    h.unregisterRider <- sub
}

// Driver read pump
func (c *DriverClient) readPump(hub *LocationHub) {
    defer func() {
        hub.unregisterDriver <- c
        c.Conn.Close()
    }()

    c.Conn.SetReadLimit(512)
    c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
    c.Conn.SetPongHandler(func(string) error {
        c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
        return nil
    })

    for {
        var update LocationUpdate
        err := c.Conn.ReadJSON(&update)
        if err != nil {
            break
        }

        update.DriverID = c.ID
        update.Timestamp = time.Now()
        c.LastSeen = time.Now()

        // Broadcast to all riders of this ride
        hub.broadcast <- update

        // Log for debugging
        log.Printf("📍 Driver %s position: %.6f, %.6f", c.ID, update.Latitude, update.Longitude)
    }
}

// Driver write pump
func (c *DriverClient) writePump() {
    ticker := time.NewTicker(30 * time.Second)
    defer func() {
        ticker.Stop()
        c.Conn.Close()
    }()

    for {
        select {
        case message, ok := <-c.Send:
            if !ok {
                c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
                return
            }
            c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
            if err := c.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
                return
            }
        case <-ticker.C:
            c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
            if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
                return
            }
        }
    }
}

// Rider write pump
func (s *RiderSubscription) writePump() {
    ticker := time.NewTicker(30 * time.Second)
    defer func() {
        ticker.Stop()
        s.Conn.Close()
    }()

    for {
        select {
        case message, ok := <-s.Send:
            if !ok {
                s.Conn.WriteMessage(websocket.CloseMessage, []byte{})
                return
            }
            s.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
            if err := s.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
                return
            }
        case <-ticker.C:
            s.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
            if err := s.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
                return
            }
        }
    }
}

// Health check endpoint
func healthHandler(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"status":"ok"}`))
}

func main() {
    godotenv.Load()

    hub := newLocationHub()
    go hub.run()

    // WebSocket endpoints
    http.HandleFunc("/ws/driver", hub.handleDriverWS)
    http.HandleFunc("/ws/rider", hub.handleRiderWS)

    // Health endpoint
    http.HandleFunc("/health", healthHandler)

    port := os.Getenv("PORT")
    if port == "" {
        port = "8080"
    }

    go func() {
        log.Printf("✅ Location Service (WebSocket) running on port %s", port)
        log.Printf("   Driver WebSocket: ws://localhost:%s/ws/driver?driver_id=XXX", port)
        log.Printf("   Rider WebSocket: ws://localhost:%s/ws/rider?ride_id=XXX", port)
        log.Fatal(http.ListenAndServe(":"+port, nil))
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    log.Println("Location Service stopped")
}
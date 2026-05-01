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

    pb "github.com/uber-clone/chat-service/proto"
)

// Message represents a chat message
type Message struct {
    ID             string    `gorm:"primaryKey"`
    ConversationID string    `gorm:"index;not null"`
    SenderID       string    `gorm:"index;not null"`
    ReceiverID     string    `gorm:"index;not null"`
    MessageType    string    `gorm:"default:'text'"` // text, image, location
    Content        string    `gorm:"type:text"`
    ImageURL       string
    Latitude       float64
    Longitude      float64
    IsRead         bool      `gorm:"default:false"`
    ReadAt         *time.Time
    DeletedBy      string    `gorm:"index"`
    CreatedAt      time.Time
}

// Conversation represents a chat thread between two users
type Conversation struct {
    ID             string    `gorm:"primaryKey"`
    Participant1   string    `gorm:"index;not null"`
    Participant2   string    `gorm:"index;not null"`
    LastMessage    string    `gorm:"type:text"`
    LastMessageAt  time.Time
    UnreadCount1   int       `gorm:"default:0"`
    UnreadCount2   int       `gorm:"default:0"`
    CreatedAt      time.Time
    UpdatedAt      time.Time
}

// ChatHub manages WebSocket connections
type ChatHub struct {
    clients    map[string]*websocket.Conn // userID -> connection
    clientsMu  sync.RWMutex
    register   chan *websocket.Conn
    unregister chan *websocket.Conn
    broadcast  chan *Message
    upgrader   websocket.Upgrader
    db         *gorm.DB
}

// ChatServer handles gRPC requests
type ChatServer struct {
    pb.UnimplementedChatServiceServer
    DB  *gorm.DB
    hub *ChatHub
}

// NewChatHub creates a new WebSocket hub
func NewChatHub(db *gorm.DB) *ChatHub {
    return &ChatHub{
        clients:    make(map[string]*websocket.Conn),
        register:   make(chan *websocket.Conn),
        unregister: make(chan *websocket.Conn),
        broadcast:  make(chan *Message, 256),
        upgrader: websocket.Upgrader{
            CheckOrigin: func(r *http.Request) bool { return true },
        },
        db: db,
    }
}

// Run starts the WebSocket hub
func (h *ChatHub) Run() {
    for {
        select {
        case conn := <-h.register:
            h.handleRegister(conn)
        case conn := <-h.unregister:
            h.handleUnregister(conn)
        case msg := <-h.broadcast:
            h.handleBroadcast(msg)
        }
    }
}

func (h *ChatHub) handleRegister(conn *websocket.Conn) {
    // Extract user ID from query params (already validated)
    userID := conn.RemoteAddr().String() // In production: get from JWT or query param
    h.clientsMu.Lock()
    h.clients[userID] = conn
    h.clientsMu.Unlock()
    log.Printf("✅ User %s connected to chat", userID)
}

func (h *ChatHub) handleUnregister(conn *websocket.Conn) {
    userID := conn.RemoteAddr().String()
    h.clientsMu.Lock()
    delete(h.clients, userID)
    h.clientsMu.Unlock()
    conn.Close()
    log.Printf("❌ User %s disconnected from chat", userID)
}

func (h *ChatHub) handleBroadcast(msg *Message) {
    h.clientsMu.RLock()
    defer h.clientsMu.RUnlock()

    // Send to receiver if online
    if conn, ok := h.clients[msg.ReceiverID]; ok {
        data, _ := json.Marshal(msg)
        conn.WriteMessage(websocket.TextMessage, data)
    }
}

// HandleWebSocket upgrades HTTP to WebSocket
func (h *ChatHub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
    userID := r.URL.Query().Get("user_id")
    if userID == "" {
        http.Error(w, "user_id required", http.StatusBadRequest)
        return
    }

    conn, err := h.upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Printf("WebSocket upgrade error: %v", err)
        return
    }

    // Store connection with user ID
    h.clientsMu.Lock()
    h.clients[userID] = conn
    h.clientsMu.Unlock()

    // Read messages from client
    for {
        var msgData map[string]interface{}
        err := conn.ReadJSON(&msgData)
        if err != nil {
            break
        }

        // Save message to database
        message := &Message{
            ID:             generateID(),
            ConversationID: msgData["conversation_id"].(string),
            SenderID:       userID,
            ReceiverID:     msgData["receiver_id"].(string),
            MessageType:    msgData["message_type"].(string),
            Content:        msgData["content"].(string),
            CreatedAt:      time.Now(),
        }

        h.db.Create(message)

        // Update conversation last message
        h.db.Model(&Conversation{}).Where("id = ?", message.ConversationID).Updates(map[string]interface{}{
            "last_message":    message.Content,
            "last_message_at": time.Now(),
            "updated_at":      time.Now(),
        })

        // Broadcast to receiver if online
        h.broadcast <- message
    }

    // Clean up on disconnect
    h.clientsMu.Lock()
    delete(h.clients, userID)
    h.clientsMu.Unlock()
}

// GetOrCreateConversation gets or creates a conversation between two users
func (s *ChatServer) GetOrCreateConversation(ctx context.Context, req *pb.GetOrCreateConversationRequest) (*pb.ConversationResponse, error) {
    var conv Conversation
    // Check if conversation exists
    result := s.DB.Where(
        "(participant1 = ? AND participant2 = ?) OR (participant1 = ? AND participant2 = ?)",
        req.UserId1, req.UserId2, req.UserId2, req.UserId1,
    ).First(&conv)

    if result.Error != nil {
        // Create new conversation
        conv = Conversation{
            ID:           generateID(),
            Participant1: req.UserId1,
            Participant2: req.UserId2,
            CreatedAt:    time.Now(),
            UpdatedAt:    time.Now(),
        }
        if err := s.DB.Create(&conv).Error; err != nil {
            return nil, status.Error(codes.Internal, "failed to create conversation")
        }
    }

    return &pb.ConversationResponse{
        Id:           conv.ID,
        Participant1: conv.Participant1,
        Participant2: conv.Participant2,
        LastMessage:  conv.LastMessage,
        LastMessageAt: conv.LastMessageAt.Unix(),
    }, nil
}

// SendMessage sends a message (when WebSocket is not available)
func (s *ChatServer) SendMessage(ctx context.Context, req *pb.SendMessageRequest) (*pb.MessageResponse, error) {
    message := &Message{
        ID:             generateID(),
        ConversationID: req.ConversationId,
        SenderID:       req.SenderId,
        ReceiverID:     req.ReceiverId,
        MessageType:    req.MessageType,
        Content:        req.Content,
        ImageURL:       req.ImageUrl,
        Latitude:       req.Latitude,
        Longitude:      req.Longitude,
        CreatedAt:      time.Now(),
    }

    if err := s.DB.Create(message).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to send message")
    }

    // Update conversation
    s.DB.Model(&Conversation{}).Where("id = ?", req.ConversationId).Updates(map[string]interface{}{
        "last_message":    req.Content,
        "last_message_at": time.Now(),
        "updated_at":      time.Now(),
    })

    // Increment unread count for receiver
    var conv Conversation
    s.DB.Where("id = ?", req.ConversationId).First(&conv)
    if conv.Participant1 == req.ReceiverId {
        conv.UnreadCount1++
    } else {
        conv.UnreadCount2++
    }
    s.DB.Save(&conv)

    // Broadcast via WebSocket if receiver is online
    s.hub.broadcast <- message

    return &pb.MessageResponse{
        Id:       message.ID,
        Content:  message.Content,
        CreatedAt: message.CreatedAt.Unix(),
    }, nil
}

// GetMessages retrieves messages for a conversation
func (s *ChatServer) GetMessages(ctx context.Context, req *pb.GetMessagesRequest) (*pb.MessagesResponse, error) {
    var messages []Message
    query := s.DB.Where("conversation_id = ?", req.ConversationId).Order("created_at DESC")

    offset := (req.Page - 1) * req.PageSize
    if err := query.Offset(int(offset)).Limit(int(req.PageSize)).Find(&messages).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to get messages")
    }

    var pbMessages []*pb.MessageResponse
    for _, m := range messages {
        pbMessages = append(pbMessages, &pb.MessageResponse{
            Id:        m.ID,
            SenderId:  m.SenderID,
            Content:   m.Content,
            MessageType: m.MessageType,
            ImageUrl:  m.ImageURL,
            IsRead:    m.IsRead,
            CreatedAt: m.CreatedAt.Unix(),
        })
    }

    return &pb.MessagesResponse{Messages: pbMessages}, nil
}

// MarkAsRead marks messages as read
func (s *ChatServer) MarkAsRead(ctx context.Context, req *pb.MarkAsReadRequest) (*pb.Empty, error) {
    if err := s.DB.Model(&Message{}).Where("conversation_id = ? AND receiver_id = ? AND is_read = ?", req.ConversationId, req.UserId, false).Updates(map[string]interface{}{
        "is_read": true,
        "read_at": time.Now(),
    }).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to mark messages as read")
    }

    // Reset unread count
    var conv Conversation
    s.DB.Where("id = ?", req.ConversationId).First(&conv)
    if conv.Participant1 == req.UserId {
        conv.UnreadCount1 = 0
    } else {
        conv.UnreadCount2 = 0
    }
    s.DB.Save(&conv)

    return &pb.Empty{}, nil
}

// ListConversations lists all conversations for a user
func (s *ChatServer) ListConversations(ctx context.Context, req *pb.ListConversationsRequest) (*pb.ConversationsResponse, error) {
    var conversations []Conversation
    if err := s.DB.Where("participant1 = ? OR participant2 = ?", req.UserId, req.UserId).Order("updated_at DESC").Find(&conversations).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list conversations")
    }

    var pbConversations []*pb.ConversationResponse
    for _, c := range conversations {
        otherParticipant := c.Participant1
        if otherParticipant == req.UserId {
            otherParticipant = c.Participant2
        }
        unreadCount := 0
        if c.Participant1 == req.UserId {
            unreadCount = c.UnreadCount1
        } else {
            unreadCount = c.UnreadCount2
        }
        pbConversations = append(pbConversations, &pb.ConversationResponse{
            Id:           c.ID,
            Participant1: c.Participant1,
            Participant2: c.Participant2,
            LastMessage:  c.LastMessage,
            LastMessageAt: c.LastMessageAt.Unix(),
            UnreadCount:  int32(unreadCount),
        })
    }

    return &pb.ConversationsResponse{Conversations: pbConversations}, nil
}

// DeleteMessage deletes a message (soft delete)
func (s *ChatServer) DeleteMessage(ctx context.Context, req *pb.DeleteMessageRequest) (*pb.Empty, error) {
    if err := s.DB.Model(&Message{}).Where("id = ?", req.MessageId).Update("deleted_by", req.UserId).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to delete message")
    }
    return &pb.Empty{}, nil
}

func generateID() string {
    return "chat_" + time.Now().Format("20060102150405") + "_" + randomString(6)
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
        dsn = "host=postgres user=postgres password=postgres dbname=chatdb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&Message{}, &Conversation{})

    hub := NewChatHub(db)
    go hub.Run()

    // HTTP WebSocket endpoint
    http.HandleFunc("/ws/chat", hub.HandleWebSocket)

    go func() {
        log.Println("✅ Chat Service WebSocket running on port 8086")
        log.Fatal(http.ListenAndServe(":8086", nil))
    }()

    grpcServer := grpc.NewServer()
    pb.RegisterChatServiceServer(grpcServer, &ChatServer{DB: db, hub: hub})

    lis, err := net.Listen("tcp", ":50071")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Chat Service gRPC running on port 50071")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("Chat Service stopped")
}
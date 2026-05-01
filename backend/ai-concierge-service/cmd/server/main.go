package main

import (
    "context"
    "encoding/json"
    "log"
    "net"
    "os"
    "os/signal"
    "strings"
    "syscall"
    "time"

    "github.com/joho/godotenv"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"

    pb "github.com/uber-clone/ai-concierge-service/proto"
)

// Conversation represents a chat session with the AI
type Conversation struct {
    ID        string    `gorm:"primaryKey"`
    UserID    string    `gorm:"index;not null"`
    Messages  string    `gorm:"type:text"` // JSON array of messages
    Context   string    `gorm:"type:text"` // JSON context (location, preferences)
    CreatedAt time.Time
    UpdatedAt time.Time
}

// Intent represents a parsed user intent
type Intent struct {
    Action      string                 // book_ride, order_food, order_grocery, send_parcel, book_appointment, check_status
    Parameters  map[string]interface{}
    Confidence  float64
}

// AIConciergeServer handles gRPC requests
type AIConciergeServer struct {
    pb.UnimplementedAIConciergeServiceServer
    // In production: OpenAI/GPT client, other service clients
}

// ProcessMessage processes a user message and returns AI response
func (s *AIConciergeServer) ProcessMessage(ctx context.Context, req *pb.ProcessMessageRequest) (*pb.ProcessMessageResponse, error) {
    message := req.Message
    userID := req.UserId

    // Parse intent from message (simplified NLP)
    intent := s.parseIntent(message)

    var response string
    var actions []*pb.Action

    switch intent.Action {
    case "book_ride":
        response, actions = s.handleBookRide(intent, req.Context)
    case "order_food":
        response, actions = s.handleOrderFood(intent, req.Context)
    case "order_grocery":
        response, actions = s.handleOrderGrocery(intent, req.Context)
    case "send_parcel":
        response, actions = s.handleSendParcel(intent, req.Context)
    case "book_appointment":
        response, actions = s.handleBookAppointment(intent, req.Context)
    case "check_status":
        response, actions = s.handleCheckStatus(intent, req.Context)
    case "cancel":
        response, actions = s.handleCancel(intent, req.Context)
    default:
        response = s.getFallbackResponse()
    }

    return &pb.ProcessMessageResponse{
        Response: response,
        Actions:  actions,
        Intent:   intent.Action,
    }, nil
}

// parseIntent extracts intent from user message (simplified rule-based)
func (s *AIConciergeServer) parseIntent(message string) Intent {
    msg := strings.ToLower(message)

    intent := Intent{
        Parameters: make(map[string]interface{}),
        Confidence: 0.8,
    }

    // Ride booking patterns
    if strings.Contains(msg, "ride") || strings.Contains(msg, "go to") || strings.Contains(msg, "pick me up") {
        intent.Action = "book_ride"
        // Extract destination
        if strings.Contains(msg, "to ") {
            parts := strings.Split(msg, "to ")
            if len(parts) > 1 {
                intent.Parameters["destination"] = strings.Split(parts[1], " ")[0]
            }
        }
        // Extract time
        if strings.Contains(msg, "at ") {
            parts := strings.Split(msg, "at ")
            if len(parts) > 1 {
                intent.Parameters["time"] = parts[1]
            }
        }
    } else if strings.Contains(msg, "food") || strings.Contains(msg, "hungry") || strings.Contains(msg, "order food") {
        intent.Action = "order_food"
        // Extract cuisine or restaurant
        cuisines := []string{"pizza", "burger", "sushi", "indian", "chinese", "italian"}
        for _, c := range cuisines {
            if strings.Contains(msg, c) {
                intent.Parameters["cuisine"] = c
                break
            }
        }
    } else if strings.Contains(msg, "grocery") || strings.Contains(msg, "groceries") || strings.Contains(msg, "shopping") {
        intent.Action = "order_grocery"
        // Extract items
        if strings.Contains(msg, "milk") {
            intent.Parameters["items"] = []string{"milk"}
        }
    } else if strings.Contains(msg, "parcel") || strings.Contains(msg, "send") || strings.Contains(msg, "deliver package") {
        intent.Action = "send_parcel"
    } else if strings.Contains(msg, "appointment") || strings.Contains(msg, "book") && strings.Contains(msg, "salon") {
        intent.Action = "book_appointment"
    } else if strings.Contains(msg, "status") || strings.Contains(msg, "where is") {
        intent.Action = "check_status"
    } else if strings.Contains(msg, "cancel") {
        intent.Action = "cancel"
    }

    return intent
}

// handleBookRide creates actions for booking a ride
func (s *AIConciergeServer) handleBookRide(intent Intent, contextJSON string) (string, []*pb.Action) {
    destination := "your destination"
    if dest, ok := intent.Parameters["destination"]; ok {
        destination = dest.(string)
    }

    response := "I'll help you book a ride to " + destination + ". "

    actions := []*pb.Action{
        {
            Type: "open_screen",
            Data: map[string]string{
                "screen": "ride_request",
                "destination": destination,
            },
        },
    }

    return response, actions
}

// handleOrderFood creates actions for ordering food
func (s *AIConciergeServer) handleOrderFood(intent Intent, contextJSON string) (string, []*pb.Action) {
    cuisine := "popular"
    if c, ok := intent.Parameters["cuisine"]; ok {
        cuisine = c.(string)
    }

    response := "I'll find " + cuisine + " restaurants near you. "

    actions := []*pb.Action{
        {
            Type: "open_screen",
            Data: map[string]string{
                "screen": "food_restaurants",
                "cuisine": cuisine,
            },
        },
    }

    return response, actions
}

// handleOrderGrocery creates actions for ordering groceries
func (s *AIConciergeServer) handleOrderGrocery(intent Intent, contextJSON string) (string, []*pb.Action) {
    response := "I'll help you order groceries. "

    actions := []*pb.Action{
        {
            Type: "open_screen",
            Data: map[string]string{
                "screen": "grocery_stores",
            },
        },
    }

    return response, actions
}

// handleSendParcel creates actions for sending a parcel
func (s *AIConciergeServer) handleSendParcel(intent Intent, contextJSON string) (string, []*pb.Action) {
    response := "I'll help you send a parcel. "

    actions := []*pb.Action{
        {
            Type: "open_screen",
            Data: map[string]string{
                "screen": "courier_create",
            },
        },
    }

    return response, actions
}

// handleBookAppointment creates actions for booking an appointment
func (s *AIConciergeServer) handleBookAppointment(intent Intent, contextJSON string) (string, []*pb.Action) {
    response := "I'll help you book an appointment. "

    actions := []*pb.Action{
        {
            Type: "open_screen",
            Data: map[string]string{
                "screen": "appointment_booking",
            },
        },
    }

    return response, actions
}

// handleCheckStatus creates actions for checking status
func (s *AIConciergeServer) handleCheckStatus(intent Intent, contextJSON string) (string, []*pb.Action) {
    response := "Let me check the status of your recent orders and rides. "

    actions := []*pb.Action{
        {
            Type: "open_screen",
            Data: map[string]string{
                "screen": "order_history",
            },
        },
    }

    return response, actions
}

// handleCancel creates actions for cancelling
func (s *AIConciergeServer) handleCancel(intent Intent, contextJSON string) (string, []*pb.Action) {
    response := "I can help you cancel your last booking. "

    actions := []*pb.Action{
        {
            Type: "cancel_last",
            Data: map[string]string{},
        },
    }

    return response, actions
}

// getFallbackResponse returns a generic response
func (s *AIConciergeServer) getFallbackResponse() string {
    responses := []string{
        "I'm not sure I understood. You can say: 'book a ride to the airport', 'order pizza', 'send a parcel', or 'book an appointment'.",
        "How can I help you today? I can book rides, order food, send parcels, or schedule appointments.",
        "I didn't catch that. Try saying something like 'book a ride home' or 'order groceries'.",
    }
    return responses[time.Now().UnixNano()%int64(len(responses))]
}

// GetSuggestions returns suggested actions based on context
func (s *AIConciergeServer) GetSuggestions(ctx context.Context, req *pb.GetSuggestionsRequest) (*pb.SuggestionsResponse, error) {
    suggestions := []*pb.Suggestion{
        {Title: "Book a ride home", Action: "book_ride", Parameters: "{\"destination\":\"home\"}"},
        {Title: "Order food nearby", Action: "order_food", Parameters: "{}"},
        {Title: "Send a parcel", Action: "send_parcel", Parameters: "{}"},
        {Title: "Book a massage", Action: "book_appointment", Parameters: "{\"service\":\"massage\"}"},
        {Title: "Track my order", Action: "check_status", Parameters: "{}"},
    }

    return &pb.SuggestionsResponse{Suggestions: suggestions}, nil
}

// ExecuteAction executes a suggested action
func (s *AIConciergeServer) ExecuteAction(ctx context.Context, req *pb.ExecuteActionRequest) (*pb.ExecuteActionResponse, error) {
    // This would call the respective service based on action type
    // For MVP, return a confirmation
    return &pb.ExecuteActionResponse{
        Success:   true,
        Message:   "Action executed successfully",
        ResultData: "{}",
    }, nil
}

// GetConversationHistory retrieves conversation history for a user
func (s *AIConciergeServer) GetConversationHistory(ctx context.Context, req *pb.GetConversationHistoryRequest) (*pb.ConversationHistoryResponse, error) {
    // In production: query from database
    return &pb.ConversationHistoryResponse{
        Messages: []*pb.ChatMessage{},
    }, nil
}

func main() {
    godotenv.Load()

    // In production: initialize database and OpenAI client
    // db, err := gorm.Open(...)
    // openAIClient := openai.NewClient(os.Getenv("OPENAI_API_KEY"))

    grpcServer := grpc.NewServer()
    pb.RegisterAIConciergeServiceServer(grpcServer, &AIConciergeServer{})

    lis, err := net.Listen("tcp", ":50072")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ AI Concierge Service running on port 50072")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("AI Concierge Service stopped")
}
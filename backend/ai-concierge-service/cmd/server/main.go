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

type Intent struct {
    Action     string
    Parameters map[string]interface{}
    Confidence float64
}

type AIConciergeServer struct {
    pb.UnimplementedAIConciergeServiceServer
}

// ProcessMessage - Process user message and return AI response
func (s *AIConciergeServer) ProcessMessage(ctx context.Context, req *pb.ProcessMessageRequest) (*pb.ProcessMessageResponse, error) {
    message := req.Message
    intent := s.parseIntent(message)

    var response string
    var actions []*pb.Action

    switch intent.Action {
    case "book_ride":
        response, actions = s.handleBookRide(intent)
    case "order_food":
        response, actions = s.handleOrderFood(intent)
    case "order_grocery":
        response, actions = s.handleOrderGrocery(intent)
    case "send_parcel":
        response, actions = s.handleSendParcel(intent)
    case "book_appointment":
        response, actions = s.handleBookAppointment(intent)
    case "check_status":
        response, actions = s.handleCheckStatus(intent)
    default:
        response = s.getFallbackResponse()
    }

    return &pb.ProcessMessageResponse{
        Response: response,
        Actions:  actions,
        Intent:   intent.Action,
    }, nil
}

func (s *AIConciergeServer) parseIntent(message string) Intent {
    msg := strings.ToLower(message)
    intent := Intent{
        Parameters: make(map[string]interface{}),
        Confidence: 0.8,
    }

    if strings.Contains(msg, "ride") || strings.Contains(msg, "go to") || strings.Contains(msg, "pick me up") {
        intent.Action = "book_ride"
        if strings.Contains(msg, "to ") {
            parts := strings.Split(msg, "to ")
            if len(parts) > 1 {
                intent.Parameters["destination"] = strings.Split(parts[1], " ")[0]
            }
        }
        if strings.Contains(msg, "at ") {
            parts := strings.Split(msg, "at ")
            if len(parts) > 1 {
                intent.Parameters["time"] = parts[1]
            }
        }
    } else if strings.Contains(msg, "food") || strings.Contains(msg, "hungry") {
        intent.Action = "order_food"
        cuisines := []string{"pizza", "burger", "sushi", "indian", "chinese", "italian"}
        for _, c := range cuisines {
            if strings.Contains(msg, c) {
                intent.Parameters["cuisine"] = c
                break
            }
        }
    } else if strings.Contains(msg, "grocery") || strings.Contains(msg, "groceries") {
        intent.Action = "order_grocery"
    } else if strings.Contains(msg, "parcel") || strings.Contains(msg, "send") {
        intent.Action = "send_parcel"
    } else if strings.Contains(msg, "appointment") || (strings.Contains(msg, "book") && strings.Contains(msg, "salon")) {
        intent.Action = "book_appointment"
    } else if strings.Contains(msg, "status") || strings.Contains(msg, "where is") {
        intent.Action = "check_status"
    }

    return intent
}

func (s *AIConciergeServer) handleBookRide(intent Intent) (string, []*pb.Action) {
    destination := "your destination"
    if dest, ok := intent.Parameters["destination"]; ok {
        destination = dest.(string)
    }
    response := "I'll help you book a ride to " + destination + ". "
    actions := []*pb.Action{{
        Type: "open_screen",
        Data: map[string]string{"screen": "ride_request", "destination": destination},
    }}
    return response, actions
}

func (s *AIConciergeServer) handleOrderFood(intent Intent) (string, []*pb.Action) {
    cuisine := "popular"
    if c, ok := intent.Parameters["cuisine"]; ok {
        cuisine = c.(string)
    }
    response := "I'll find " + cuisine + " restaurants near you. "
    actions := []*pb.Action{{
        Type: "open_screen",
        Data: map[string]string{"screen": "food_restaurants", "cuisine": cuisine},
    }}
    return response, actions
}

func (s *AIConciergeServer) handleOrderGrocery(intent Intent) (string, []*pb.Action) {
    response := "I'll help you order groceries. "
    actions := []*pb.Action{{Type: "open_screen", Data: map[string]string{"screen": "grocery_stores"}}}
    return response, actions
}

func (s *AIConciergeServer) handleSendParcel(intent Intent) (string, []*pb.Action) {
    response := "I'll help you send a parcel. "
    actions := []*pb.Action{{Type: "open_screen", Data: map[string]string{"screen": "courier_create"}}}
    return response, actions
}

func (s *AIConciergeServer) handleBookAppointment(intent Intent) (string, []*pb.Action) {
    response := "I'll help you book an appointment. "
    actions := []*pb.Action{{Type: "open_screen", Data: map[string]string{"screen": "appointment_booking"}}}
    return response, actions
}

func (s *AIConciergeServer) handleCheckStatus(intent Intent) (string, []*pb.Action) {
    response := "Let me check the status of your recent orders and rides. "
    actions := []*pb.Action{{Type: "open_screen", Data: map[string]string{"screen": "order_history"}}}
    return response, actions
}

func (s *AIConciergeServer) getFallbackResponse() string {
    responses := []string{
        "I'm not sure I understood. You can say: 'book a ride to the airport', 'order pizza', 'send a parcel', or 'book an appointment'.",
        "How can I help you today? I can book rides, order food, send parcels, or schedule appointments.",
    }
    return responses[time.Now().UnixNano()%int64(len(responses))]
}

// GetSuggestions - Get suggested actions
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

// ExecuteAction - Execute a suggested action
func (s *AIConciergeServer) ExecuteAction(ctx context.Context, req *pb.ExecuteActionRequest) (*pb.ExecuteActionResponse, error) {
    return &pb.ExecuteActionResponse{
        Success:    true,
        Message:    "Action executed successfully",
        ResultData: "{}",
    }, nil
}

// GetConversationHistory - Get conversation history
func (s *AIConciergeServer) GetConversationHistory(ctx context.Context, req *pb.GetConversationHistoryRequest) (*pb.ConversationHistoryResponse, error) {
    return &pb.ConversationHistoryResponse{Messages: []*pb.ChatMessage{}}, nil
}

func main() {
    godotenv.Load()

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
}
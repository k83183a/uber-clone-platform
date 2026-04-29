package main
import ("context";"log";"net";"os";"os/signal";"syscall";"time";"github.com/joho/godotenv";"google.golang.org/grpc";"google.golang.org/grpc/codes";"google.golang.org/grpc/status";"gorm.io/driver/postgres";"gorm.io/gorm")
type Ride struct{ID,RiderID,DriverID,Status,RideType string;PickupLat,PickupLng,DropoffLat,DropoffLng,Fare float64;CreatedAt time.Time}
type RideServer struct{*gorm.DB}
func (s *RideServer) RequestRide(ctx context.Context, req *pb.RequestRideRequest)(*pb.RideResponse,error){
        r:=Ride{ID:"ride_"+time.Now().Format("20060102150405"),RiderID:req.RiderId,Status:"pending",PickupLat:req.PickupLat,PickupLng:req.PickupLng,DropoffLat:req.DropoffLat,DropoffLng:req.DropoffLng,RideType:req.RideType,Fare:req.FareEstimate,CreatedAt:time.Now()}
            s.DB.Create(&r)
                return &pb.RideResponse{Id:r.ID,Status:r.Status,Fare:r.Fare},nil
}
func main(){godotenv.Load();dsn:=os.Getenv("DB_DSN");if dsn==""{dsn="host=postgres user=postgres password=postgres dbname=ridedb port=5432 sslmode=disable"}
db,_:=gorm.Open(postgres.Open(dsn),&gorm.Config{});db.AutoMigrate(&Ride{})
s:=grpc.NewServer();pb.RegisterRideServiceServer(s,&RideServer{DB:db})
l,_:=net.Listen("tcp",":50053");go s.Serve(l);log.Println("✅ Ride on :50053")
q:=make(chan os.Signal,1);signal.Notify(q,syscall.SIGINT,syscall.SIGTERM);<-q;s.GracefulStop()}

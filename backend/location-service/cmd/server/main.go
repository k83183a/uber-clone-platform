package main
import ("log";"net/http";"os";"os/signal";"syscall";"github.com/gorilla/websocket";"github.com/joho/godotenv")
var u=websocket.Upgrader{CheckOrigin:func(r*http.Request)bool{return true}}
func main(){godotenv.Load();http.HandleFunc("/ws/driver",func(w http.ResponseWriter,r*http.Request){c,_:=u.Upgrade(w,r,nil);defer c.Close();for{_,m,_:=c.ReadMessage();log.Printf("Driver: %s",m)}});http.HandleFunc("/ws/rider",func(w http.ResponseWriter,r*http.Request){c,_:=u.Upgrade(w,r,nil);defer c.Close();for{}})
go func(){log.Println("✅ Location on :8080");log.Fatal(http.ListenAndServe(":8080",nil))}()
q:=make(chan os.Signal,1);signal.Notify(q,syscall.SIGINT,syscall.SIGTERM);<-q}

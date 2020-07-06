package main

import (
	"container/list"
	"log"
	"net/http"
	"strings"
	"time"

	socketio "github.com/googollee/go-socket.io" // socket.io 패키지 사용
)

var (
	chatRooms = make(map[string]Channels)
)

// Channels : each chat room's sub, unsub, publish channels
type Channels struct {
	subscribe   chan (chan<- Subscription) // 구독 채널
	unsubscribe chan (<-chan Event)        // 구독 해지 채널
	publish     chan Event                 // 이벤트 발행 채널
	userList    map[string]string
}

// Event 채팅 이벤트 구조체 정의
type Event struct {
	EvtType   string // 이벤트 타입
	User      string // 사용자 이름
	Timestamp int    // 시간 값
	Text      string // 메시지 텍스트
}

// Subscription 구독 구조체 정의
type Subscription struct {
	Archive []Event      // 지금까지 쌓인 이벤트를 저장할 슬라이스
	New     <-chan Event // 새 이벤트가 생길 때마다 데이터를 받을 수 있도록
	// 이벤트 채널 생성
}

// NewEvent 이벤트 생성 함수
func NewEvent(evtType, user, msg string) Event {
	return Event{evtType, user, int(time.Now().Unix()), msg}
}

// Subscribe 새로운 사용자가 들어왔을 때 이벤트를 구독할 함수
func Subscribe(key string) Subscription {
	c := make(chan Subscription)  // 채널을 생성하여
	chatRooms[key].subscribe <- c // 구독 채널에 보냄
	return <-c
}

// Cancel 사용자가 나갔을 때 구독을 취소할 함수
func (s Subscription) Cancel(key string) {
	chatRooms[key].unsubscribe <- s.New // 구독 해지 채널에 보냄

	for { // 무한 루프
		select {
		case _, ok := <-s.New: // 채널에서 값을 모두 꺼냄
			if !ok { // 값을 모두 꺼냈으면 함수를 빠져나옴
				return
			}
		default:
			return
		}
	}
}

// Join 사용자가 들어왔을 때 이벤트 발행
func Join(user, key string) {
	chatRooms[key].publish <- NewEvent("join", user, "")
}

// Say 사용자가 채팅 메시지를 보냈을 때 이벤트 발행
func Say(user, message, key string) {
	chatRooms[key].publish <- NewEvent("message", user, message)
}

// Leave 사용자가 나갔을 때 이벤트 발행
func Leave(user, key string) {
	chatRooms[key].publish <- NewEvent("leave", user, "")
}

// Chatroom 구독, 구독 해지, 발행 된 이벤트를 처리할 함수
func Chatroom(ch Channels) {
	archive := list.New()     // 쌓인 이벤트를 저장할 연결 리스트
	subscribers := list.New() // 구독자 목록을 저장할 연결 리스트

	//fmt.Println(ch)

	for {
		select {
		case c := <-ch.subscribe: // 새로운 사용자가 들어왔을 때
			var events []Event

			// 쌓인 이벤트가 있다면
			for e := archive.Front(); e != nil; e = e.Next() {
				// events 슬라이스에 이벤트를 저장
				events = append(events, e.Value.(Event))
			}

			subscriber := make(chan Event, 10) // 이벤트 채널 생성
			subscribers.PushBack(subscriber)   // 이벤트 채널을 구독자 목록에
			// 추가

			c <- Subscription{events, subscriber} // 구독 구조체 인스턴스를
			// 생성하여 채널 c에 보냄

		case event := <-ch.publish: // 새 이벤트가 발행되었을 때
			// 모든 사용자에게 이벤트 전달
			for e := subscribers.Front(); e != nil; e = e.Next() {
				// 구독자 목록에서 이벤트 채널을 꺼냄
				subscriber := e.Value.(chan Event)

				// 방금 받은 이벤트를 이벤트 채널에 보냄
				subscriber <- event
			}

			// 저장된 이벤트 개수가 20개가 넘으면
			if archive.Len() >= 20 {
				archive.Remove(archive.Front()) // 이벤트 삭제
			}
			archive.PushBack(event) // 현재 이벤트를 저장

		case c := <-ch.unsubscribe: // 사용자가 나갔을 때
			for e := subscribers.Front(); e != nil; e = e.Next() {
				subscriber := e.Value.(chan Event) // 구독자 목록에서 이벤트 채널을 꺼냄

				if subscriber == c { // 구독자 목록에 들어있는 이벤트와 채널 c가 같으면
					subscribers.Remove(e) // 구독자 목록에서 삭제
					break
				}
			}
		}
	}
}

func main() {
	server, err := socketio.NewServer(nil) // socker.io 초기화
	if err != nil {
		log.Fatal(err)
	}

	// 웹 브라우저에서 socket.io로 접속했을 때 실행할 콜백 설정
	server.On("connection", func(so socketio.Socket) {
		//fmt.Println("Socket " + so.Id() + "connected")

		so.On("register", func(src string) {
			// split data
			data := strings.Split(src, "|") // data: [room_id id pw]
			roomID := data[0]
			userID := data[1]
			userPw := data[2]
			//fmt.Println(roomID, userID, userPw)

			// check if chat room with entered room_id exists
			v, exists := chatRooms[roomID]
			if !exists {
				// if don't, make a new one
				//fmt.Println("Make a new room " + roomID)
				newChannel := Channels{make(chan (chan<- Subscription)), make(chan (<-chan Event)), make(chan Event), make(map[string]string)}
				//fmt.Println("New Channel is made: ", newChannel)
				chatRooms[roomID] = newChannel
				//fmt.Println("Assign it into chatRooms")
				go Chatroom(chatRooms[roomID]) // 채팅방을 처리할 함수를 고루틴으로 실행

				// add user to the userlist
				//fmt.Println("Add user " + userID + " to " + roomID)
				chatRooms[roomID].userList[userID] = userPw
			} else {
				// else, add user to the userlist
				//fmt.Println("Add user " + userID + " to " + roomID)
				v.userList[userID] = userPw
			}
		})

		so.On("login", func(src string) {
			// split data
			data := strings.Split(src, "|")
			roomID := data[0]
			userID := data[1]
			userPw := data[2]
			//fmt.Println(roomID, userID, userPw)

			// check if chat room with entered room_id exists
			v, exists := chatRooms[roomID]
			if !exists {
				// if don't, send error message
				msg := "Chat room with room id " + roomID + " doesn't exist"
				so.Emit("error", msg)
			} else {
				// check if user is in the userList
				pw, userExists := v.userList[userID]
				if !userExists {
					msg := "You're not found in the user list. Please register first"
					so.Emit("error", msg)
				} else {
					// check if the password is correct
					if pw != userPw {
						msg := "Password is incorrect!"
						so.Emit("error", msg)
					} else {
						// login to chat room
						newMessages := make(chan string)

						// 웹 브라우저가 접속되면
						s := Subscribe(roomID) // 구독 처리
						Join(userID, roomID)   // 사용자가 채팅방에 들어왔다는 이벤트 발행

						for _, event := range s.Archive { // 지금까지 쌓인 이벤트를
							so.Emit("event", event) // 웹 브라우저로 접속한 사용자에게 보냄
						}

						// 웹 브라우저에서 보내오는 채팅 메시지를 받을 수 있도록 콜백 설정
						so.On("message", func(msg string) {
							newMessages <- msg
						})

						// 웹 브라우저의 접속이 끊어졌을 때 콜백 설정
						so.On("disconnection", func() {
							Leave(userID, roomID)
							s.Cancel(roomID)
						})

						go func() {
							for {
								select {
								case event := <-s.New: // 채널에 이벤트가 들어오면
									so.Emit("event", event) // 이벤트 데이터를 웹 브라우저에 보냄

								case msg := <-newMessages: // 웹 브라우저에서 채팅 메시지를 보내오면
									Say(userID, msg, roomID) // 채팅 메시지 이벤트 발행
								}
							}
						}()
					}
				}

			}

		})
	})

	http.Handle("/socket.io/", server) // /socket.io/ 경로는
	// socket.io 인스턴스가 처리하도록 설정

	http.Handle("/", http.FileServer(http.Dir("."))) // 현재 디렉터리를 파일 서버로 설정

	http.ListenAndServe(":11111", nil) // 80번 포트에서 웹 서버 실행
}

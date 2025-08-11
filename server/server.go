package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type ChatMessage struct {
	Type string `json:"type"`
	Name string `json:"name"`
	Room string `json:"room"`
	Body string `json:"body"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type MsgType struct {
	Type string `json:"type"`
}

type JoinRequest struct {
	Type     string `json:"type"`
	UserName string `json:"username"`
	Pub_Key  string `json:"pub_key"`
}

// Hub maintains the set of active clients and broadcasts messages to the clients.
type Hub struct {
	clients    map[*websocket.Conn]string
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	broadcast  chan ChatMessage
	mu         sync.Mutex
}

type PendingRequest struct {
	Username  string `json:"username"`
	Pub_Key   string `json:"pub_key"`   // PEM-encoded
	Timestamp int64  `json:"timestamp"` // Unix time for tracking
}

type ApprovedUser struct {
	Username string `json:"username"`
	Pub_Key  string `json:"pub_key"` // PEM-encoded
}

var pendingRequests []PendingRequest
var approvedUsers []ApprovedUser

func getConfigFilePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	appDir := filepath.Join(configDir, "ghostwire")
	os.MkdirAll(appDir, 0700)
	return appDir, nil
}

func commandLoop() {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")
	for scanner.Scan() {
		line := scanner.Text()
		handleCommand(line)
	}
	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading commands:", err)
	}
}

func handleCommand(cmd string) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return
	}
	switch parts[0] {
	case "approve":
		if len(parts) < 2 {
			fmt.Println("Usage: approve <username> ")
			return
		}
		if parts[1] == "list" {
			if len(pendingRequests) < 1 {
				fmt.Println("No pending requests")
			} else {
				for _, request := range pendingRequests {
					fmt.Println(request.Username)
				}
			}
		} else {
			userNameToApprove := parts[1]
			var foundUser bool = false
			for index, request := range pendingRequests {
				if request.Username == userNameToApprove {
					approveUser(index)
					foundUser = true
					break
				}
			}
			if !foundUser {
				fmt.Printf("User \"%s\" not found \n", userNameToApprove)
			}
		}

	case "ban":
		if len(parts) < 2 {
			fmt.Println("Usage: ban <username>")
			return
		}
		//username := parts[1]
		//banUser(username)
	case "list":
		//listUsers()
	case "help":
		fmt.Println("Commands: approve <username>, ban <username>, list, help")
	default:
		fmt.Println("Unknown command:", parts[0])
	}
}

func approveUser(index int) {
	fmt.Printf("Approving user: %s \n", pendingRequests[index].Username)

	var user ApprovedUser
	user.Username = pendingRequests[index].Username
	user.Pub_Key = pendingRequests[index].Pub_Key

	approvedUsers = append(approvedUsers, user)

	path, _ := getConfigFilePath()
	path = filepath.Join(path, "approved_users.json")

	err := saveApprovedUsers(path)
	if err != nil {
		fmt.Printf("Error writing saved user list: %s", err)
	}
}

func saveApprovedUsers(filename string) error {
	// Marshal slice to indented JSON for readability
	data, err := json.MarshalIndent(approvedUsers, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal approved users: %w", err)
	}

	// Write atomically: write to temp file then rename
	tmpFile := filename + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Rename temp file to actual file (atomic on most OSes)
	if err := os.Rename(tmpFile, filename); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

func loadApprovedUsers() error {
	path, _ := getConfigFilePath()
	path = filepath.Join(path, "approved_users.json")

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// No file yet — return empty slice, not an error
	}

	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read approved users file: %w", err)
	}

	// Unmarshal JSON into slice
	if err := json.Unmarshal(data, &approvedUsers); err != nil {
		return fmt.Errorf("failed to unmarshal approved users: %w", err)
	}

	for _, user := range approvedUsers {
		fmt.Printf("Found approval for %s \n", user.Username)
	}

	return nil
}

func newHub() *Hub {
	return &Hub{
		clients:    make(map[*websocket.Conn]string),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
		broadcast:  make(chan ChatMessage),
	}
}

func (h *Hub) run() {
	for {
		select {
		case conn := <-h.register:
			h.mu.Lock()
			h.clients[conn] = ""
			h.mu.Unlock()
			log.Println("New client connected")
		case conn := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				conn.Close()
				log.Println("Client disconnected")
			}
			h.mu.Unlock()
		case msg := <-h.broadcast:
			h.mu.Lock()
			// Broadcast chat_message to all clients
			if msg.Type == "chat_message" {
				data, err := json.Marshal(msg)
				if err != nil {
					log.Println("Error marshaling message:", err)
					h.mu.Unlock()
					continue
				}
				for client := range h.clients {
					err := client.WriteMessage(websocket.TextMessage, data)
					if err != nil {
						log.Println("Error writing to client:", err)
						client.Close()
						delete(h.clients, client)
					}
				}
			}
			h.mu.Unlock()
		}
	}
}

func wsHandler(h *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("Upgrade error:", err)
			return
		}

		h.register <- conn

		defer func() {
			h.unregister <- conn
		}()

		for {
			_, msgBytes, err := conn.ReadMessage()
			if err != nil {
				log.Println("Read error:", err)
				break
			}

			var mt MsgType
			err = json.Unmarshal(msgBytes, &mt)
			if err != nil {
				// handle error
			}
			fmt.Println(mt.Type)

			switch mt.Type {
			case "chat_message":
				var msg ChatMessage
				err = json.Unmarshal(msgBytes, &msg)
				if err != nil {
					log.Println("JSON unmarshal error:", err)
					continue
				}
				h.broadcast <- msg

			case "join_request":
				var join_request JoinRequest
				err = json.Unmarshal(msgBytes, &join_request)
				if err != nil {
					log.Println("JSON unmarshal error:", err)
					continue
				}

				var request PendingRequest
				err = json.Unmarshal(msgBytes, &request)
				if err != nil {
					log.Println("JSON unmarshal error:", err)
					continue
				}
				request.Timestamp = time.Now().Unix()
				pendingRequests = append(pendingRequests, request)
			}
		}
	}
}

func main() {
	hub := newHub()

	loadApprovedUsers()

	go hub.run()
	go commandLoop()

	http.HandleFunc("/ws", wsHandler(hub))

	fmt.Println("Server started on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe:", err)
	}

}

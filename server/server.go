package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

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

// Hub maintains the set of active clients and broadcasts messages to the clients.
type Hub struct {
	clients    map[*websocket.Conn]string
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	broadcast  chan ChatMessage
	mu         sync.Mutex
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

			var msg ChatMessage
			err = json.Unmarshal(msgBytes, &msg)
			if err != nil {
				log.Println("JSON unmarshal error:", err)
				continue
			}

			// For now, broadcast chat_message only
			if msg.Type == "chat_message" {
				h.broadcast <- msg
			}
		}
	}
}

func main() {
	hub := newHub()
	go hub.run()

	http.HandleFunc("/ws", wsHandler(hub))

	fmt.Println("Server started on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}
